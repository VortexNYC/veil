// Command drillrestore proves a restored dump is a live vault. It opens the
// store against a restored database and exercises the production unwrap path:
// org_keys under the KEK, the owner wrap under the master, the item blob under
// the owner DEK. -wrongkek asserts the same reads fail closed. Secret bytes
// are never printed — only pass/fail and ciphertext-free metadata.
package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/VortexNYC/veil/internal/crypto"
	"github.com/VortexNYC/veil/internal/store"
)

func main() {
	dsn := flag.String("dsn", "", "DSN of the restored (scratch) database")
	kekFile := flag.String("kek", "", "path to KEK file: 64 hex chars or raw 32 bytes")
	item := flag.String("item", "", "item id to decrypt through the full chain")
	org := flag.String("org", "", "org id expected to hold an org_keys row")
	wrongKEK := flag.Bool("wrongkek", false, "corrupt the KEK: every unwrap must fail")
	flag.Parse()
	if *dsn == "" || *kekFile == "" || *item == "" || *org == "" {
		flag.Usage()
		os.Exit(2)
	}

	raw, err := os.ReadFile(*kekFile)
	if err != nil {
		fmt.Println("DRILL FAIL kek read:", err)
		os.Exit(1)
	}
	kek, err := parseKEK(raw)
	if err != nil {
		fmt.Println("DRILL FAIL kek parse:", err)
		os.Exit(1)
	}
	if *wrongKEK {
		kek[0] ^= 0xff
	}

	st, err := store.OpenPostgres(*dsn, kek)
	if err != nil {
		fmt.Println("DRILL FAIL open:", err)
		os.Exit(1)
	}
	defer st.Close()
	ctx := context.Background()

	hasKey, err := st.HasOrgKey(ctx, *org)
	if err != nil || !hasKey {
		fmt.Println("DRILL FAIL org_keys row missing for", *org, err)
		os.Exit(1)
	}
	fmt.Println("org_keys row present:", *org)

	sec, err := st.Secret(*item)
	if *wrongKEK {
		if err == nil {
			fmt.Println("DRILL FAIL wrong KEK decrypted a secret")
			os.Exit(1)
		}
		fmt.Println("wrong KEK fails closed:", err)
		fmt.Println("DRILL PASS (fail-closed)")
		return
	}
	if err != nil {
		fmt.Println("DRILL FAIL secret unwrap:", err)
		os.Exit(1)
	}
	if len(sec) == 0 {
		fmt.Println("DRILL FAIL secret decrypted to zero bytes")
		os.Exit(1)
	}
	fmt.Println("item decrypts under escrowed KEK:", *item)
	fmt.Println("DRILL PASS")
}

func parseKEK(raw []byte) ([]byte, error) {
	s := strings.TrimSpace(string(raw))
	if len(s) == 2*crypto.KeySize {
		return hex.DecodeString(s)
	}
	if len(raw) == crypto.KeySize {
		return raw, nil
	}
	return nil, fmt.Errorf("want %d raw bytes or %d hex chars, got %d", crypto.KeySize, 2*crypto.KeySize, len(raw))
}
