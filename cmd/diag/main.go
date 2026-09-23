// Command diag seeds a single session against PG_TEST_DSN and fires one
// POST /v1/use at LOADTEST_ORIGINS[0], printing the raw response. Throwaway
// diagnostic for the Railway multi-host run.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
)

func main() {
	dsn := os.Getenv("PG_TEST_DSN")
	origin := os.Getenv("DIAG_ORIGIN")
	upstream := os.Getenv("DIAG_UPSTREAM")

	if q := os.Getenv("DIAG_SQL"); q != "" {
		conn, err := pgx.Connect(context.Background(), dsn)
		if err != nil {
			fmt.Println("connect:", err)
			os.Exit(1)
		}
		defer conn.Close(context.Background())
		rows, err := conn.Query(context.Background(), q)
		if err != nil {
			fmt.Println("query:", err)
			os.Exit(1)
		}
		defer rows.Close()
		for rows.Next() {
			vals, _ := rows.Values()
			fmt.Println(vals)
		}
		return
	}
	var tokens []string
	var itemID string
	if tf := os.Getenv("DIAG_TOKENS_FILE"); tf != "" {
		raw, err := os.ReadFile(tf)
		if err != nil {
			fmt.Println("tokens file:", err)
			os.Exit(1)
		}
		for _, t := range strings.Split(string(raw), "\n") {
			if t = strings.TrimSpace(t); t != "" {
				tokens = append(tokens, t)
			}
		}
		itemID = os.Getenv("DIAG_ITEM")
		fmt.Printf("replaying %d tokens item=%s\n", len(tokens), itemID)
	} else {
		a, err := app.OpenPostgres(dsn)
		if err != nil {
			fmt.Println("open:", err)
			os.Exit(1)
		}
		defer a.Close()

		agent, err := a.AddAgent("diag-agent")
		if err != nil {
			fmt.Println("agent:", err)
			os.Exit(1)
		}
		item, err := a.AddItem("diag-item", upstream, []byte("diag-secret"))
		if err != nil {
			fmt.Println("item:", err)
			os.Exit(1)
		}
		if _, err := a.AddGrant(agent.ID, item.ID, protocol.Level2); err != nil {
			fmt.Println("grant:", err)
			os.Exit(1)
		}
		itemID = item.ID
		human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: a.HumanID, OrgID: a.OrgID}
		nsess := 1
		if s := os.Getenv("DIAG_SESSIONS"); s != "" {
			nsess, _ = strconv.Atoi(s)
		}
		tokens = make([]string, nsess)
		var sMu sync.Mutex
		var sWg sync.WaitGroup
		sSem := make(chan struct{}, 16)
		for i := 0; i < nsess; i++ {
			sWg.Add(1)
			sSem <- struct{}{}
			go func(i int) {
				defer sWg.Done()
				defer func() { <-sSem }()
				_, t, err := a.CreateSession(human, agent.ID, time.Hour, 0)
				if err != nil {
					fmt.Println("session:", err)
					return
				}
				sMu.Lock()
				tokens[i] = t
				sMu.Unlock()
			}(i)
		}
		sWg.Wait()
		fmt.Printf("sessions=%d agent=%s item=%s\n", nsess, agent.ID, item.ID)
	}
	tok := tokens[0]
	fmt.Printf("token prefix=%q len=%d\n", tok[:min(12, len(tok))], len(tok))
	fmt.Println("isSessionToken path:", app.IsSessionToken(tok))

	body, _ := json.Marshal(map[string]any{"item": itemID, "url": upstream, "method": "GET"})

	if n := os.Getenv("DIAG_BURST"); n != "" {
		count, _ := strconv.Atoi(n)
		var mu sync.Mutex
		codes := map[string]int{}
		var wg sync.WaitGroup
		conc := 300
		if c := os.Getenv("DIAG_CONC"); c != "" {
			conc, _ = strconv.Atoi(c)
		}
		sem := make(chan struct{}, conc)
		client := &http.Client{Timeout: 15 * time.Second}
		for i := 0; i < count; i++ {
			wg.Add(1)
			sem <- struct{}{}
			go func(i int) {
				defer wg.Done()
				defer func() { <-sem }()
				req, _ := http.NewRequest(http.MethodPost, origin+"/v1/use", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+tokens[i%len(tokens)])
				resp, err := client.Do(req)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					codes["ERR:"+err.Error()[:min(80, len(err.Error()))]]++
					return
				}
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
				resp.Body.Close()
				codes[fmt.Sprintf("%d %s", resp.StatusCode, strings.TrimSpace(string(b)))]++
			}(i)
		}
		wg.Wait()
		for k, v := range codes {
			fmt.Printf("%6d  %s\n", v, k)
		}
		return
	}

	req, _ := http.NewRequest(http.MethodPost, origin+"/v1/use", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("post:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	fmt.Printf("status=%d body=%s\n", resp.StatusCode, string(out))
}
