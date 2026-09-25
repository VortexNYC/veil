package publicapi

import (
	"bufio"
	"net/http"
	"testing"
	"time"

	"github.com/VortexNYC/veil/internal/protocol"
)

// TestStreamRequestsAuth gates the feed: the stream carries org signals, so
// it gets the same owner requirement as the list it mirrors.
func TestStreamRequestsAuth(t *testing.T) {
	srv := apiServer(t, testApp(t))
	for _, tc := range []struct {
		token string
		want  int
	}{
		{"", http.StatusUnauthorized},
		{"member", http.StatusForbidden},
	} {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/requests/stream", nil)
		if tc.token != "" {
			req.Header.Set("Authorization", "Bearer "+tc.token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != tc.want {
			t.Fatalf("token %q: got %d, want %d", tc.token, res.StatusCode, tc.want)
		}
	}
}

// TestStreamRequestsTicks proves the end-to-end shape: an owner-held stream
// gets a data frame when an ask files in their org, and never for another
// org's write. The frame carries no payload — data: {} — by contract.
func TestStreamRequestsTicks(t *testing.T) {
	a := testApp(t)
	srv := apiServer(t, a)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/requests/stream", nil)
	req.Header.Set("Authorization", "Bearer human")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q", ct)
	}

	frames := make(chan string, 8)
	go func() {
		sc := bufio.NewScanner(res.Body)
		var ev string
		for sc.Scan() {
			line := sc.Text()
			if line == "" {
				if ev != "" {
					frames <- ev
					ev = ""
				}
				continue
			}
			ev += line + "\n"
		}
	}()

	// The connected comment arrives first — it also proves headers flushed.
	select {
	case f := <-frames:
		if f != ": connected\n" {
			t.Fatalf("first frame %q", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no connected frame")
	}

	// A foreign org's ask must not tick this owner's stream.
	if _, err := a.Store.FileRequest(protocol.ApprovalRequest{
		ID: "req-other", OrgID: "org-other", AgentID: "x", ItemID: "y",
		GrantID: "g-other", Action: protocol.ActionFetch, Status: protocol.RequestOpen,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-frames:
		t.Fatalf("foreign-org write leaked a frame: %q", f)
	case <-time.After(300 * time.Millisecond):
	}

	// An ask in the owner's org ticks with an empty data frame.
	if _, err := a.Store.FileRequest(protocol.ApprovalRequest{
		ID: "req-mine", OrgID: protocol.LocalOrgID, AgentID: "x", ItemID: "y",
		GrantID: "g-mine", Action: protocol.ActionFetch, Status: protocol.RequestOpen,
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-frames:
		if f != "data: {}\n" {
			t.Fatalf("tick frame %q", f)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no tick for org write")
	}
}
