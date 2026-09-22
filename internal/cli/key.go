package cli

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/VortexNYC/veil/internal/crypto"
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

	c.AddCommand(rotateOrg, rotateKEK)
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
