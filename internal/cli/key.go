package cli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
	"github.com/openbao/openbao/sdk/v2/helper/shamir"
	"github.com/spf13/cobra"
)

// keyCmd is the origin key-lifecycle surface: org-master rotation and
// deployment-KEK rotation. Both run directly against Postgres — rotation is
// an ops verb on the store, not an HTTP call. Keys come from env vars or
// files, never argv.
func keyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "key",
		Short: "Rotate org masters and the deployment KEK (Postgres origin only)",
	}
	var dsn, kekFile string
	persistent := func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&dsn, "dsn", "", "Postgres DSN (default env VEIL_POSTGRES_DSN)")
		cmd.Flags().StringVar(&kekFile, "kek-file", "", "file containing the current KEK as hex (default env VEIL_KEK)")
	}
	resolve := func() (string, []byte, error) {
		if dsn == "" {
			dsn = os.Getenv("VEIL_POSTGRES_DSN")
		}
		if dsn == "" {
			return "", nil, fmt.Errorf("key: --dsn or VEIL_POSTGRES_DSN required")
		}
		kek, err := loadHexKey("VEIL_KEK", kekFile)
		if err != nil {
			return "", nil, err
		}
		return dsn, kek, nil
	}

	var newKekFile, sharesDir string
	var generateNew bool
	var shareCount, shareThreshold int
	rotateOrg := &cobra.Command{
		Use:   "rotate-org ORG",
		Short: "Mint a fresh org master and rewrap the org's owner DEKs",
		Long: "Mints a fresh master for ORG, rewraps every owner DEK under it " +
			"(item ciphertexts seal under owner DEKs and are untouched), and " +
			"bumps org_keys.key_version — atomically. Concurrent rotations fail " +
			"one side; retry. After commit, every origin replica must drop its " +
			"cached master — redeploy, or restart, since cache invalidation is " +
			"per-process.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			if err := s.RotateOrgKey(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "rotated org %s: new master, owner DEKs rewrapped, key_version bumped\n", args[0])
			return nil
		},
	}
	persistent(rotateOrg)

	rotateKEK := &cobra.Command{
		Use:   "rotate-kek",
		Short: "Rewrap every org master under a new deployment KEK",
		Long: "Unwraps each org_keys row under the current KEK (VEIL_KEK or " +
			"--kek-file) and rewraps it under the new KEK (VEIL_KEK_NEW, " +
			"--new-kek-file, or --generate) in one transaction. Org masters, " +
			"owner DEKs, and item ciphertexts do not change. Any row that " +
			"fails to unwrap aborts the whole rotation — a backup KEK that " +
			"opens nothing is not a KEK. With --generate --shares N " +
			"--threshold M, the new KEK is minted and Shamir-split into " +
			"share-<i>-<fp>.hex files in --shares-dir (staged .tmp, finalized " +
			"only after the rotation commits) — the assembled key never " +
			"needs to exist as a stored artifact; ops reconstructs it " +
			"('veil key combine') only to set VEIL_KEK. " +
			"After commit, update VEIL_KEK on every origin replica and redeploy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if generateNew && (newKekFile != "" || os.Getenv("VEIL_KEK_NEW") != "") {
				return fmt.Errorf("key: --generate is exclusive with --new-kek-file/VEIL_KEK_NEW")
			}
			// A minted KEK exists only in memory — rotating without emitting
			// shares would leave every org_keys row wrapped under a key
			// nobody holds.
			if generateNew && shareCount == 0 {
				return fmt.Errorf("key: --generate requires --shares (a minted KEK must be born split)")
			}
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			var newKEK []byte
			if generateNew {
				newKEK = make([]byte, crypto.KeySize)
				if _, err := rand.Read(newKEK); err != nil {
					return fmt.Errorf("key: generate: %w", err)
				}
			} else {
				newKEK, err = loadHexKey("VEIL_KEK_NEW", newKekFile)
				if err != nil {
					return err
				}
			}
			// Emit shares before committing: if the split or the write fails,
			// nothing has rotated and no shares of a live key are stranded.
			var staged []string
			if shareCount > 0 {
				if sharesDir == "" {
					return fmt.Errorf("key: --shares requires --shares-dir")
				}
				staged, err = stageShares(newKEK, shareCount, shareThreshold, sharesDir)
				if err != nil {
					return err
				}
				defer func() {
					for _, f := range staged {
						_ = os.Remove(f)
					}
				}()
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			if err := s.RotateKEK(cmd.Context(), newKEK); err != nil {
				return err
			}
			wantShares := len(staged) > 0
			for _, f := range staged {
				if err := os.Rename(f, strings.TrimSuffix(f, ".tmp")); err != nil {
					staged = nil // keep every share artifact for ops
					return fmt.Errorf("key: rotation committed but share finalize failed — files remain in %s: %w", sharesDir, err)
				}
			}
			staged = nil
			fmt.Fprintln(cmd.OutOrStdout(), "rotated KEK: every org_keys row rewrapped; set VEIL_KEK to the new key on all replicas")
			if wantShares {
				fmt.Fprintf(cmd.OutOrStdout(), "shares written to %s — distribute to custodians, then delete the dir\n", sharesDir)
			}
			return nil
		},
	}
	migrateKeysvc := &cobra.Command{
		Use:   "migrate-keysvc",
		Short: "Re-seal legacy local-AEAD org_keys rows to the remote key service",
		Long: "One-shot cutover for the Cloudflare key-service migration: unwraps " +
			"each org_keys row under the local VEIL_KEK, re-seals it through " +
			"keys.veil.nyc (version-0x02 envelope), and writes it back. Requires " +
			"VEIL_KEYSVC_URL + VEIL_KEYSVC_TOKEN set and the local VEIL_KEK still " +
			"present. Rows already service-wrapped are skipped (idempotent). " +
			"After verification, remove VEIL_KEK from every replica.",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			if err := s.MigrateToKeyService(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "migrated org_keys rows to key-service envelopes — verify then remove VEIL_KEK")
			return nil
		},
	}
	persistent(migrateKeysvc)

	remoteRotate := &cobra.Command{
		Use:   "rotate-kek-remote",
		Short: "Rotate the KEK via the key-service dual-key window",
		Long: "Service-side KEK rotation for VEIL_KEYSVC_* mode: bind VEIL_KEK_NEXT " +
			"on the Worker BEFORE running this (wrangler secrets-store), then this " +
			"sends ciphertext-only through /rewrap and commits — origin never holds " +
			"the new key. Unbind VEIL_KEK_NEXT after success.",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			if err := s.RotateKEKRemote(cmd.Context()); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "remote KEK rotation committed — unbind VEIL_KEK_NEXT on the Worker")
			return nil
		},
	}
	persistent(remoteRotate)

	persistent(rotateKEK)
	rotateKEK.Flags().StringVar(&newKekFile, "new-kek-file", "", "file containing the new KEK as hex (default env VEIL_KEK_NEW)")
	rotateKEK.Flags().BoolVar(&generateNew, "generate", false, "mint the new KEK (requires --shares; exclusive with --new-kek-file/VEIL_KEK_NEW)")
	rotateKEK.Flags().IntVar(&shareCount, "shares", 0, "Shamir-split the new KEK into N share files (0 = off)")
	rotateKEK.Flags().IntVar(&shareThreshold, "threshold", 2, "shares needed to reconstruct (with --shares)")
	rotateKEK.Flags().StringVar(&sharesDir, "shares-dir", "", "directory for share-*.hex files (created mode 700)")

	var ownerKind, ownerID, recoveryFile string
	var expires time.Duration
	ownerFlags := func(cmd *cobra.Command) {
		cmd.Flags().StringVar(&ownerKind, "owner-kind", "", "owner kind (e.g. user, org)")
		cmd.Flags().StringVar(&ownerID, "owner-id", "", "owner id")
		cmd.Flags().StringVar(&recoveryFile, "recovery-file", "", "file containing the recovery key as hex (required)")
		_ = cmd.MarkFlagRequired("owner-kind")
		_ = cmd.MarkFlagRequired("owner-id")
		_ = cmd.MarkFlagRequired("recovery-file")
	}

	storeRecovery := &cobra.Command{
		Use:   "store-recovery ORG",
		Short: "Escrow the org master under owner-held recovery material",
		Long: "Seals ORG's committed master under the recovery key in " +
			"--recovery-file and upserts the recovery_wraps row. The recovery " +
			"key never persists — the owner keeps it offline. Re-minting " +
			"replaces the wrap and clears used_at. --expires bounds how long " +
			"the wrap stays openable.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			recoveryKey, err := loadHexKey("recovery-file", recoveryFile)
			if err != nil {
				return err
			}
			var exp time.Time
			if expires > 0 {
				exp = time.Now().UTC().Add(expires)
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			o := protocol.Owner{Kind: protocol.OwnerKind(ownerKind), ID: ownerID}
			if err := s.StoreRecoveryWrap(cmd.Context(), args[0], o, recoveryKey, exp); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "recovery wrap stored for %s/%s on org %s\n", ownerKind, ownerID, args[0])
			return nil
		},
	}
	persistent(storeRecovery)
	ownerFlags(storeRecovery)
	storeRecovery.Flags().DurationVar(&expires, "expires", 0, "wrap lifetime (e.g. 720h); 0 = no expiry")

	recoverOrg := &cobra.Command{
		Use:   "recover-org ORG",
		Short: "Recover the org master from a wrap and re-seed it under the current KEK",
		Long: "Lost-KEK / lost-devices recovery: verifies --recovery-file " +
			"against the owner's wrap, re-seals the recovered master under " +
			"this deployment's KEK, and stamps the wrap used — atomically, in " +
			"one transaction, so a failed reseed cannot burn the wrap. Run it " +
			"with the NEW VEIL_KEK already set; the recovered vault then " +
			"decrypts every item it held before the loss.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			recoveryKey, err := loadHexKey("recovery-file", recoveryFile)
			if err != nil {
				return err
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			o := protocol.Owner{Kind: protocol.OwnerKind(ownerKind), ID: ownerID}
			if err := s.RecoverOrgKey(cmd.Context(), args[0], o, recoveryKey); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "org %s recovered: master re-seeded under the current KEK; mint a new recovery wrap now\n", args[0])
			return nil
		},
	}
	persistent(recoverOrg)
	ownerFlags(recoverOrg)

	var combineDir, combineOut string
	combine := &cobra.Command{
		Use:   "combine",
		Short: "Reconstruct a KEK from Shamir share files (custody ceremony)",
		Long: "Reads share-*.hex files in --shares-dir, reconstructs the KEK " +
			"with any threshold-sufficient subset, and writes the hex key to " +
			"--out (mode 600). The assembled key goes to Railway VEIL_KEK and " +
			"the file is deleted — shares are the custody artifact, not this.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ents, err := os.ReadDir(combineDir)
			if err != nil {
				return fmt.Errorf("key: %w", err)
			}
			// Only files from ONE ceremony: share-<i>-<fp>.hex where fp is
			// identical across every file. A stray .hex or a share from a
			// different split interpolates to a wrong key with no error —
			// the fingerprint in the name is what binds the set.
			re := shareFileRe
			fps := map[string]bool{}
			var parts [][]byte
			for _, e := range ents {
				m := re.FindStringSubmatch(e.Name())
				if m == nil {
					if strings.HasSuffix(e.Name(), ".hex") {
						return fmt.Errorf("key: %s is not a share-<i>-<fp>.hex — refusing to mix sets", e.Name())
					}
					continue
				}
				fps[m[2]] = true
				raw, err := os.ReadFile(filepath.Join(combineDir, e.Name()))
				if err != nil {
					return err
				}
				b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
				if err != nil {
					return fmt.Errorf("key: %s is not valid hex: %w", e.Name(), err)
				}
				parts = append(parts, b)
			}
			if len(fps) > 1 {
				return fmt.Errorf("key: shares from multiple ceremonies in %s — split the dirs", combineDir)
			}
			if len(parts) < 2 {
				return fmt.Errorf("key: need at least 2 share-<i>-<fp>.hex files in %s", combineDir)
			}
			key, err := shamir.Combine(parts)
			if err != nil {
				return fmt.Errorf("key: combine: %w", err)
			}
			if len(key) != crypto.KeySize {
				return fmt.Errorf("key: combined %d bytes, want %d — wrong shares?", len(key), crypto.KeySize)
			}
			var fp string
			for f := range fps {
				fp = f
			}
			if shareFingerprint(key) != fp {
				return fmt.Errorf("key: reconstructed key fingerprint does not match share names — set is corrupt")
			}
			if combineOut == "" {
				return fmt.Errorf("key: --out required")
			}
			// OpenFile+Chmod: WriteFile's perm applies only on create; a
			// pre-existing world-readable --out would keep its mode with a
			// reconstructed KEK inside.
			out, err := os.OpenFile(combineOut, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
			if err != nil {
				return fmt.Errorf("key: %w", err)
			}
			if err := out.Chmod(0o600); err != nil {
				out.Close()
				return fmt.Errorf("key: %w", err)
			}
			if _, err := fmt.Fprintln(out, hex.EncodeToString(key)); err != nil {
				out.Close()
				return fmt.Errorf("key: %w", err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("key: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "reconstructed KEK from %d shares → %s\n", len(parts), combineOut)
			return nil
		},
	}
	combine.Flags().StringVar(&combineDir, "shares-dir", "", "directory holding share-*.hex files")
	combine.Flags().StringVar(&combineOut, "out", "", "file to write the reconstructed hex key (mode 600)")
	_ = combine.MarkFlagRequired("shares-dir")
	_ = combine.MarkFlagRequired("out")

	// VEIL-72: a staged share set co-located on one machine is not
	// custody. escrow-seal wraps ONE share to ONE custodian's X25519
	// pubkey so the parcel can travel and rest on that custodian's
	// machine; escrow-open is the custodian side. Shares stay the
	// threshold artifact — a sealed parcel never holds the whole KEK.
	var kgDir string
	escrowKeygen := &cobra.Command{
		Use:   "escrow-keygen",
		Short: "Mint a custodian X25519 keypair for KEK-share escrow",
		Long: "The CUSTODIAN runs this on their own machine — the private " +
			"key never travels. Writes custodian.pub (hex, safe to hand to " +
			"the founder) and custodian.key (hex, mode 600) in --out-dir " +
			"(mode 700), then prints the pubkey. The founder seals one " +
			"Shamir share to it with 'veil key escrow-seal'. Refuses to " +
			"overwrite: a replaced key orphans every parcel already sealed " +
			"to it.",
		RunE: func(cmd *cobra.Command, args []string) error {
			pub, priv, err := device.Generate()
			if err != nil {
				return fmt.Errorf("key: %w", err)
			}
			if err := os.MkdirAll(kgDir, 0o700); err != nil {
				return fmt.Errorf("key: %w", err)
			}
			if err := os.Chmod(kgDir, 0o700); err != nil {
				return fmt.Errorf("key: %w", err)
			}
			pubFile := filepath.Join(kgDir, "custodian.pub")
			keyFile := filepath.Join(kgDir, "custodian.key")
			if err := writeKeyFileExclusive(pubFile, []byte(hex.EncodeToString(pub)+"\n")); err != nil {
				return err
			}
			if err := writeKeyFileExclusive(keyFile, []byte(hex.EncodeToString(priv)+"\n")); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "custodian pubkey: %s\n", hex.EncodeToString(pub))
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s (share) and %s (keep private, mode 600)\n", pubFile, keyFile)
			return nil
		},
	}
	escrowKeygen.Flags().StringVar(&kgDir, "out-dir", "", "directory for custodian.pub/custodian.key (created mode 700)")
	_ = escrowKeygen.MarkFlagRequired("out-dir")

	var sealIn, sealTo, sealToFile, sealOut string
	escrowSeal := &cobra.Command{
		Use:   "escrow-seal",
		Short: "Seal one Shamir KEK share to a custodian pubkey (founder side)",
		Long: "Reads share-<i>-<fp>.hex (the canonical stageShares name — " +
			"ordinal and ceremony fingerprint are taken from it), packs a " +
			"VEILSHR1 envelope, and seals it to the custodian's X25519 " +
			"pubkey with nacl/box SealAnonymous. The parcel is opaque " +
			"without the custodian's private key and holds one share only " +
			"— it is safe to send over an authenticated channel. Verify " +
			"the pubkey fingerprint with the custodian out-of-band before " +
			"sealing. Refuses non-canonical share names and existing " +
			"--out files.",
		RunE: func(cmd *cobra.Command, args []string) error {
			m := shareFileRe.FindStringSubmatch(filepath.Base(sealIn))
			if m == nil {
				return fmt.Errorf("key: %s is not a canonical share-<i>-<fp>.hex — sealing a misnamed share mislabels custody", filepath.Base(sealIn))
			}
			ordinal, err := strconv.Atoi(m[1])
			if err != nil || ordinal < 1 {
				return fmt.Errorf("key: share ordinal %q invalid", m[1])
			}
			fpHex := m[2]
			raw, err := os.ReadFile(sealIn)
			if err != nil {
				return fmt.Errorf("key: %w", err)
			}
			share, err := hex.DecodeString(strings.TrimSpace(string(raw)))
			if err != nil {
				return fmt.Errorf("key: %s is not valid hex: %w", sealIn, err)
			}
			// A KEK share is exactly KeySize+1 bytes (32B secret + x-coord).
			// Sealing a truncated share reports a custody handoff that can
			// never reconstruct — reject anything but the real shape.
			if len(share) != crypto.KeySize+1 {
				return fmt.Errorf("key: %s share is %d bytes, want %d (32B secret + x-coord)", sealIn, len(share), crypto.KeySize+1)
			}
			var pub []byte
			switch {
			case sealTo != "" && sealToFile != "":
				return fmt.Errorf("key: --to and --to-file are exclusive")
			case sealTo != "":
				pub, err = hex.DecodeString(strings.TrimSpace(sealTo))
			case sealToFile != "":
				var b []byte
				b, err = os.ReadFile(sealToFile)
				if err == nil {
					pub, err = hex.DecodeString(strings.TrimSpace(string(b)))
				}
			default:
				return fmt.Errorf("key: --to or --to-file required")
			}
			if err != nil {
				return fmt.Errorf("key: custodian pubkey: %w", err)
			}
			if len(pub) != device.PublicSize {
				return fmt.Errorf("key: custodian pubkey is %d bytes, want %d", len(pub), device.PublicSize)
			}
			var fp [4]byte
			if _, err := hex.Decode(fp[:], []byte(fpHex)); err != nil {
				return fmt.Errorf("key: share fingerprint %q: %w", fpHex, err)
			}
			parcel, err := device.Seal(escrowEnvelope(ordinal, fp, share), pub)
			if err != nil {
				return fmt.Errorf("key: seal: %w", err)
			}
			if err := writeKeyFileExclusive(sealOut, parcel); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "sealed share %d of ceremony %s → %s\n", ordinal, fpHex, sealOut)
			fmt.Fprintln(cmd.OutOrStdout(), "tell the custodian out-of-band: ordinal + fingerprint — they confirm both after escrow-open")
			return nil
		},
	}
	escrowSeal.Flags().StringVar(&sealIn, "in", "", "share-<i>-<fp>.hex to seal (required)")
	escrowSeal.Flags().StringVar(&sealTo, "to", "", "custodian X25519 pubkey as hex")
	escrowSeal.Flags().StringVar(&sealToFile, "to-file", "", "file containing the custodian pubkey as hex")
	escrowSeal.Flags().StringVar(&sealOut, "out", "", "parcel file to write (mode 600, must not exist)")
	_ = escrowSeal.MarkFlagRequired("in")
	_ = escrowSeal.MarkFlagRequired("out")

	var openIn, openKey, openDir string
	escrowOpen := &cobra.Command{
		Use:   "escrow-open",
		Short: "Open a sealed KEK-share parcel (custodian side)",
		Long: "Opens --in with the custodian private key in --key-file, " +
			"validates the VEILSHR1 envelope, and writes " +
			"share-<ordinal>-<fp>.hex in --out-dir (mode 700/600, refuses " +
			"overwrite) — the exact file 'veil key combine' consumes. " +
			"Confirm the printed ordinal and fingerprint against what the " +
			"founder announced before counting this as custody.",
		RunE: func(cmd *cobra.Command, args []string) error {
			priv, err := loadHexKey("key-file", openKey)
			if err != nil {
				return err
			}
			blob, err := os.ReadFile(openIn)
			if err != nil {
				return fmt.Errorf("key: %w", err)
			}
			env, err := device.Open(blob, priv)
			if err != nil {
				return fmt.Errorf("key: open parcel: %w", err)
			}
			ordinal, fp, share, err := parseEscrowEnvelope(env)
			if err != nil {
				return err
			}
			fpHex := hex.EncodeToString(fp[:])
			if err := os.MkdirAll(openDir, 0o700); err != nil {
				return fmt.Errorf("key: %w", err)
			}
			if err := os.Chmod(openDir, 0o700); err != nil {
				return fmt.Errorf("key: %w", err)
			}
			out := filepath.Join(openDir, fmt.Sprintf("share-%d-%s.hex", ordinal, fpHex))
			if err := writeKeyFileExclusive(out, []byte(hex.EncodeToString(share)+"\n")); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "opened parcel: share %d of ceremony %s → %s\n", ordinal, fpHex, out)
			return nil
		},
	}
	escrowOpen.Flags().StringVar(&openIn, "in", "", "parcel file to open (required)")
	escrowOpen.Flags().StringVar(&openKey, "key-file", "", "custodian private key file, hex (mode 600)")
	escrowOpen.Flags().StringVar(&openDir, "out-dir", "", "directory for the recovered share-<i>-<fp>.hex (created mode 700)")
	_ = escrowOpen.MarkFlagRequired("in")
	_ = escrowOpen.MarkFlagRequired("key-file")
	_ = escrowOpen.MarkFlagRequired("out-dir")

	c.AddCommand(rotateOrg, rotateKEK, storeRecovery, recoverOrg, combine,
		escrowKeygen, escrowSeal, escrowOpen)
	return c
}

// shareFileRe is the canonical stageShares name: share-<ordinal>-<fp>.hex
// where fp is the 8-hex sha256 prefix binding the set to one ceremony.
var shareFileRe = regexp.MustCompile(`^share-(\d+)-([0-9a-f]{8})\.hex$`)

// escrowMagic binds the parcel format+version. Bump the last byte on any
// incompatible envelope change; escrow-open rejects unknown magic.
const escrowMagic = "VEILSHR1"

// escrowEnvelope packs ordinal + ceremony fingerprint + share bytes into
// the payload sealed to a custodian. Metadata lives INSIDE the box
// ciphertext — a parcel cannot be relabeled into another share slot by
// renaming the file.
func escrowEnvelope(ordinal int, fp [4]byte, share []byte) []byte {
	env := make([]byte, 0, len(escrowMagic)+1+len(fp)+len(share))
	env = append(env, escrowMagic...)
	env = append(env, byte(ordinal))
	env = append(env, fp[:]...)
	return append(env, share...)
}

// parseEscrowEnvelope validates and unpacks an opened parcel. It checks
// shape, not truth — combine's fingerprint check is the last word on
// whether a share set is real.
func parseEscrowEnvelope(env []byte) (ordinal int, fp [4]byte, share []byte, err error) {
	head := len(escrowMagic) + 1 + len(fp)
	if len(env) < head+2 {
		return 0, fp, nil, fmt.Errorf("key: parcel too short for a VEILSHR1 envelope")
	}
	if string(env[:len(escrowMagic)]) != escrowMagic {
		return 0, fp, nil, fmt.Errorf("key: parcel is not a VEILSHR1 envelope")
	}
	ordinal = int(env[len(escrowMagic)])
	if ordinal < 1 {
		return 0, fp, nil, fmt.Errorf("key: parcel share ordinal %d invalid", ordinal)
	}
	copy(fp[:], env[len(escrowMagic)+1:head])
	share = env[head:]
	if len(share) != crypto.KeySize+1 {
		return 0, fp, nil, fmt.Errorf("key: parcel share is %d bytes, want %d (32B secret + x-coord)", len(share), crypto.KeySize+1)
	}
	return ordinal, fp, share, nil
}

// writeKeyFileExclusive creates a mode-600 file that must not already
// exist — custody artifacts are write-once, so a second write can never
// silently replace a delivered parcel or a recovered share.
func writeKeyFileExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("key: %s exists — delete it first (custody artifacts never overwrite)", path)
		}
		return fmt.Errorf("key: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("key: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("key: %w", err)
	}
	return nil
}

// stageShares Shamir-splits key into n shares (threshold m) and writes them
// as share-<i>-<fp>.hex.tmp in dir (mode 700, files mode 600), where fp is
// an 8-hex fingerprint of the key — a share-set binding so combine can't
// silently interpolate files from two different ceremonies. The caller
// renames them to share-<i>-<fp>.hex only after the rotation commits —
// a share of a KEK that never took effect must never look final.
func stageShares(key []byte, n, threshold int, dir string) ([]string, error) {
	if n < 2 || threshold < 2 || threshold > n {
		return nil, fmt.Errorf("key: invalid --shares/--threshold %d-of-%d", threshold, n)
	}
	parts, err := shamir.Split(key, n, threshold)
	if err != nil {
		return nil, fmt.Errorf("key: split: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("key: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("key: %w", err)
	}
	fp := shareFingerprint(key)
	var staged []string
	fail := func(err error) ([]string, error) {
		for _, done := range staged {
			_ = os.Remove(done)
		}
		return nil, err
	}
	for i, p := range parts {
		f := filepath.Join(dir, fmt.Sprintf("share-%d-%s.hex.tmp", i+1, fp))
		// Remove before write: a pre-existing loose-mode tmp file must not
		// keep its mode through overwrite — WriteFile's perm only applies
		// on create. Chmod is belt-on-top (umask can't widen 0600 anyway).
		if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
			return fail(fmt.Errorf("key: %w", err))
		}
		if err := os.WriteFile(f, []byte(hex.EncodeToString(p)+"\n"), 0o600); err != nil {
			_ = os.Remove(f)
			return fail(fmt.Errorf("key: %w", err))
		}
		if err := os.Chmod(f, 0o600); err != nil {
			_ = os.Remove(f)
			return fail(fmt.Errorf("key: %w", err))
		}
		staged = append(staged, f)
	}
	return staged, nil
}

// shareFingerprint is a short sha256 prefix of the secret — a key-id for
// matching shares to one ceremony, never a disclosure oracle (the secret
// is 256 bits of CSPRNG, not a dictionary).
func shareFingerprint(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:4])
}

// loadHexKey reads a hex-encoded key from file (preferred) or an env var.
func loadHexKey(envName, file string) ([]byte, error) {
	raw := ""
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("key: %w", err)
		}
		raw = strings.TrimSpace(string(b))
	} else {
		raw = strings.TrimSpace(os.Getenv(envName))
	}
	if raw == "" {
		return nil, fmt.Errorf("key: %s or --kek-file required", envName)
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("key: %s is not valid hex: %w", envName, err)
	}
	if len(key) != crypto.KeySize {
		return nil, fmt.Errorf("key: %s must be %d bytes (got %d)", envName, crypto.KeySize, len(key))
	}
	return key, nil
}
