package cli

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/protocol"
	"github.com/VortexNYC/veil/internal/store"
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

	var newKekFile string
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
			"--kek-file) and rewraps it under the new KEK (VEIL_KEK_NEW or " +
			"--new-kek-file) in one transaction. Org masters, owner DEKs, and " +
			"item ciphertexts do not change. Any row that fails to unwrap " +
			"aborts the whole rotation — a backup KEK that opens nothing is " +
			"not a KEK. After commit, update VEIL_KEK on every origin replica " +
			"and redeploy.",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, kek, err := resolve()
			if err != nil {
				return err
			}
			newKEK, err := loadHexKey("VEIL_KEK_NEW", newKekFile)
			if err != nil {
				return err
			}
			s, err := store.OpenPostgres(d, kek)
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()
			if err := s.RotateKEK(cmd.Context(), newKEK); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "rotated KEK: every org_keys row rewrapped; set VEIL_KEK to the new key on all replicas")
			return nil
		},
	}
	persistent(rotateKEK)
	rotateKEK.Flags().StringVar(&newKekFile, "new-kek-file", "", "file containing the new KEK as hex (default env VEIL_KEK_NEW)")

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

	c.AddCommand(rotateOrg, rotateKEK, storeRecovery, recoverOrg)
	return c
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
