package cli

// veil recovery — the member-key ceremony (VEIL-78). The private half of
// the X25519 pair lives only in recovery.key files (mode 0600); the origin
// ever sees the 32-byte pubkey and ciphertext blobs.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/device"
	"github.com/VortexNYC/veil/internal/publicapi"
)

// recoveryKeyPath is the member's escrowed X25519 private key — the "keep
// this safe" artifact (Emergency Kit role). Mode 0600, never stdout.
func recoveryKeyPath(home string) (string, error) {
	dir, err := resolveHome(home)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "recovery.key"), nil
}

// loadOrGenerateRecoveryKey reads the existing recovery private key, or
// mints one and plants it at path with 0600 — the pair enroll() publishes.
func loadOrGenerateRecoveryKey(path string) (pub, priv []byte, err error) {
	if b, rerr := readFileMaterial(path); rerr == nil && len(b) == device.PublicSize {
		priv = b
		pub, err = device.Public(priv)
		return pub, priv, err
	}
	pub, priv, err = device.Generate()
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(path, priv, 0o600); err != nil {
		return nil, nil, err
	}
	return pub, priv, nil
}

// decodeCLIKey accepts a 32-byte key as hex or base64 — the same shapes
// keygen prints and other CLIs accept.
func decodeCLIKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := hex.DecodeString(s); err == nil && len(b) == device.PublicSize {
		return b, nil
	}
	for _, dec := range []*base64.Encoding{base64.StdEncoding, base64.RawURLEncoding, base64.RawStdEncoding} {
		if b, err := dec.DecodeString(s); err == nil && len(b) == device.PublicSize {
			return b, nil
		}
	}
	return nil, fmt.Errorf("key must be a %d-byte hex or base64 string", device.PublicSize)
}

func recoveryCmd(home *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "recovery",
		Short: "Member recovery keys and the recovery ceremony. Private keys stay in files.",
	}

	var outFile string
	enroll := &cobra.Command{
		Use:   "enroll",
		Short: "Enroll your recovery share: keypair to --out-file (0600), pubkey to the origin. Owner aal2.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if outFile == "" {
				p, err := recoveryKeyPath(*home)
				if err != nil {
					return err
				}
				outFile = p
			}
			raw, err := originHumanCLI(cmd.Context())
			if err != nil {
				return err
			}
			pub, _, err := loadOrGenerateRecoveryKey(outFile)
			if err != nil {
				return err
			}
			body, _ := json.Marshal(publicapi.EnrollRecoveryRequest{
				Pubkey: base64.StdEncoding.EncodeToString(pub),
			})
			res, err := originDo(cmd.Context(), http.MethodPost, "/v1/recovery/enroll", raw, body)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "recovery key written to %s — keep it somewhere safe\n", outFile)
			var share json.RawMessage
			_ = json.Unmarshal(res, &share)
			return encode(cmd, map[string]any{
				"enrolled": true,
				"pubkey":   hex.EncodeToString(pub),
				"out_file": outFile,
			})
		},
	}
	enroll.Flags().StringVar(&outFile, "out-file", "", "write the recovery private key here (default ~/.veil/recovery.key). 0600, never stdout.")

	keygen := &cobra.Command{
		Use:   "keygen",
		Short: "Mint an offline recovery keypair — the pending-file step before an owner starts recovery for you.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if outFile == "" {
				return fmt.Errorf("recovery keygen: --out-file is required")
			}
			pub, priv, err := device.Generate()
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(outFile), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(outFile, priv, 0o600); err != nil {
				return err
			}
			return encode(cmd, map[string]string{
				"pubkey_hex":    hex.EncodeToString(pub),
				"pubkey_base64": base64.StdEncoding.EncodeToString(pub),
				"out_file":      outFile,
			})
		},
	}
	keygen.Flags().StringVar(&outFile, "out-file", "", "write the new private key here (e.g. ~/.veil/recovery.key.pending). 0600.")
	_ = keygen.MarkFlagRequired("out-file")

	var humanID, pubkeyArg string
	var resetMFA bool
	start := &cobra.Command{
		Use:   "start",
		Short: "File a recovery request. Self-start on a new device stages ~/.veil/recovery.key.pending.",
		RunE: func(cmd *cobra.Command, args []string) error {
			var pub []byte
			var err error
			if pubkeyArg != "" {
				// Owner-initiated: the member generated a keypair offline and
				// sent the pubkey — nothing stages locally.
				pub, err = decodeCLIKey(pubkeyArg)
				if err != nil {
					return err
				}
			} else {
				keyPath, err := recoveryKeyPath(*home)
				if err != nil {
					return err
				}
				pending := keyPath + ".pending"
				var priv []byte
				pub, priv, err = device.Generate()
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(pending), 0o700); err != nil {
					return err
				}
				if err := os.WriteFile(pending, priv, 0o600); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "staged new recovery key at %s — renames to recovery.key after completion\n", pending)
			}
			// Self-start may be an aal1 token — no local confirm gate.
			raw, err := originHumanTokenLive(cmd.Context())
			if err != nil {
				return err
			}
			body, _ := json.Marshal(publicapi.StartRecoveryRequest{
				HumanID:   humanID,
				NewPubkey: base64.StdEncoding.EncodeToString(pub),
				ResetMFA:  resetMFA,
			})
			res, err := originDo(cmd.Context(), http.MethodPost, "/v1/recovery/start", raw, body)
			if err != nil {
				return err
			}
			var out publicapi.StartRecoveryResponse
			if err := json.Unmarshal(res, &out); err != nil {
				return err
			}
			return encode(cmd, out)
		},
	}
	start.Flags().StringVar(&humanID, "human", "", "owner-initiated: the member identity to recover")
	start.Flags().StringVar(&pubkeyArg, "pubkey", "", "the member's new pubkey (hex or base64) — required with --human, skips local keygen")
	start.Flags().BoolVar(&resetMFA, "reset-mfa", false, "also wipe the member's Kratos second factors on completion")

	var status string
	list := &cobra.Command{
		Use:   "list",
		Short: "List org recovery requests. Owner aal2.",
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := originOwnerToken(cmd.Context())
			if err != nil {
				return err
			}
			path := "/v1/recovery"
			if status != "" {
				path += "?status=" + status
			}
			res, err := originDo(cmd.Context(), http.MethodGet, path, raw, nil)
			if err != nil {
				return err
			}
			var out publicapi.RecoveriesResponse
			if err := json.Unmarshal(res, &out); err != nil {
				return err
			}
			return encode(cmd, out)
		},
	}
	list.Flags().StringVar(&status, "status", "open", "open|completed|denied|expired|cancelled")

	show := &cobra.Command{
		Use:   "show ID",
		Short: "One recovery request — owners see the confirmer view (member email, epoch, own share).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := originHumanTokenLive(cmd.Context())
			if err != nil {
				return err
			}
			res, err := originDo(cmd.Context(), http.MethodGet, "/v1/recovery/"+args[0], raw, nil)
			if err != nil {
				return err
			}
			var out app.RecoveryView
			if err := json.Unmarshal(res, &out); err != nil {
				return err
			}
			return encode(cmd, out)
		},
	}

	var proofFile string
	complete := &cobra.Command{
		Use:   "complete ID",
		Short: "Confirm a recovery request — rotates the org master and re-wraps the member's share. Owner aal2.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, err := originOwnerToken(cmd.Context())
			if err != nil {
				return err
			}
			var proof []byte
			if proofFile == "" {
				p, err := recoveryKeyPath(*home)
				if err != nil {
					return err
				}
				if _, err := os.Stat(p); err == nil {
					proofFile = p
				}
			}
			if proofFile != "" {
				// The offline-key fast path: unwrap our own share and prove
				// the committed master — bypasses the solo-owner timelock.
				priv, err := readFileMaterial(proofFile)
				if err != nil {
					return fmt.Errorf("recovery complete: --proof-file %s: %w", proofFile, err)
				}
				share, err := originDo(cmd.Context(), http.MethodGet, "/v1/recovery/share", raw, nil)
				if err != nil {
					return err
				}
				var v app.RecoveryShareView
				if err := json.Unmarshal(share, &v); err != nil {
					return err
				}
				if !v.Enrolled || len(v.WrappedMaster) == 0 {
					return fmt.Errorf("recovery complete: no enrolled share to prove with")
				}
				master, err := device.Accept(v.WrappedMaster, priv)
				if err != nil {
					return fmt.Errorf("recovery complete: proof key does not open your share: %w", err)
				}
				mac := hmac.New(sha256.New, master)
				mac.Write([]byte(args[0]))
				proof = mac.Sum(nil)
			}
			body, _ := json.Marshal(publicapi.CompleteRecoveryRequest{
				ProofHMAC: hex.EncodeToString(proof),
			})
			res, err := originDo(cmd.Context(), http.MethodPost, "/v1/recovery/"+args[0]+"/complete", raw, body)
			if err != nil {
				return err
			}
			var out app.RecoveryView
			if err := json.Unmarshal(res, &out); err != nil {
				return err
			}
			return encode(cmd, out)
		},
	}
	complete.Flags().StringVar(&proofFile, "proof-file", "", "recovery.key to prove the committed master (default ~/.veil/recovery.key when it exists)")

	deny := &cobra.Command{
		Use:   "deny ID",
		Short: "Deny (owner) or cancel (initiator/target) an open recovery request.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// The initiator may be an aal1 locked-out member — no local gate.
			raw, err := originHumanTokenLive(cmd.Context())
			if err != nil {
				return err
			}
			res, err := originDo(cmd.Context(), http.MethodPost, "/v1/recovery/"+args[0]+"/deny", raw, nil)
			if err != nil {
				return err
			}
			var out app.RecoveryView
			if err := json.Unmarshal(res, &out); err != nil {
				return err
			}
			return encode(cmd, out)
		},
	}

	c.AddCommand(enroll, keygen, start, list, show, complete, deny)
	return c
}

// maybeAutoEnrollRecovery is the lazy backfill (VEIL-78): after a human
// login, when no recovery.key exists on this machine and the share shows
// enrolled:false, mint a keypair, escrow the private key at 0600, and post
// the pubkey. Best-effort by doctrine — a login must never fail because a
// recovery enroll did.
func maybeAutoEnrollRecovery(ctx context.Context, home, rawToken string) {
	if rawToken == "" {
		return
	}
	keyPath, err := recoveryKeyPath(home)
	if err != nil {
		return
	}
	if _, err := os.Stat(keyPath); err == nil {
		return // key material exists locally — nothing to do
	}
	res, err := originDo(ctx, http.MethodGet, "/v1/recovery/share", rawToken, nil)
	if err != nil {
		return
	}
	var share app.RecoveryShareView
	if err := json.Unmarshal(res, &share); err != nil || share.Enrolled {
		return
	}
	pub, _, err := loadOrGenerateRecoveryKey(keyPath)
	if err != nil {
		return
	}
	body, _ := json.Marshal(publicapi.EnrollRecoveryRequest{
		Pubkey: base64.StdEncoding.EncodeToString(pub),
	})
	if _, err := originDo(ctx, http.MethodPost, "/v1/recovery/enroll", rawToken, body); err != nil {
		fmt.Fprintf(os.Stderr, "recovery auto-enroll skipped: %v\n", err)
	}
}
