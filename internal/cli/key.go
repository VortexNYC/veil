package cli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
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
			re := regexp.MustCompile(`^share-\d+-([0-9a-f]{8})\.hex$`)
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
				fps[m[1]] = true
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

	c.AddCommand(rotateOrg, rotateKEK, storeRecovery, recoverOrg, combine)
	return c
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
