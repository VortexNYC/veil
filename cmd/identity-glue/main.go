package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/VortexNYC/veil/identity/glue"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	g, err := glue.NewHydra(env("HYDRA_ADMIN_URL", "http://127.0.0.1:4445"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := g.EnsureFirstParty(context.Background(), glue.FirstParty{
		ID:          env("HYDRA_CLIENT_ID", glue.DefaultClientID),
		RedirectURL: env("BROKER_REDIRECT_URL", "http://127.0.0.1:4460/oidc/callback"),
		RedirectURLs: []string{
			env("SPA_REDIRECT_URL", "https://app.veil.nyc/oidc/callback"),
			"http://127.0.0.1:4470/oidc/callback",
		},
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	addr := listenAddr()
	log.Printf("identity glue consent %s", addr)
	log.Fatal(http.ListenAndServe(addr, g))
}
