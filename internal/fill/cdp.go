// CDP fill — VOR-689. Agent-driven headless/CDP browsers have no extension:
// the host talks DevTools protocol to the page instead. The host holds the
// credential in memory and writes it into form fields via chromedp — the
// agent sees "filled N fields", never the secret.
package fill

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// CDPPort is the `--cdp <port>` value: the DevTools port of a headless Chrome
// the agent launched (e.g. `chrome --headless --remote-debugging-port=9222`).
type CDPPort int

// cdpPage is one row of /json/list — a running target the agent navigated.
type cdpPage struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

const cdpProbe = `(function(){
  const pass = document.querySelector('input[type=password]');
  let user = null;
  if (pass) {
    const form = pass.closest('form') || document;
    const inputs = form.querySelectorAll('input[type=text],input[type=email],input[type=tel],input:not([type])');
    for (const i of inputs) { user = i; break; }
    if (!user) user = document.querySelector('input[type=text],input[type=email],input[name*=user i],input[name*=email i],input[name*=login i]');
  }
  if (!user) user = document.querySelector('input[type=email],input[name*=user i],input[name*=email i],input[name*=login i]');
  function sel(el){ if(!el) return null; if(el.id) return '#'+el.id; const a=[]; if(el.name) a.push('[name="'+el.name+'"]'); if(el.autocomplete) a.push('[autocomplete="'+el.autocomplete+'"]'); return a.join('')||null; }
  return JSON.stringify({password: sel(pass), username: sel(user), url: location.href});
})()`

type cdpForm struct {
	Password string `json:"password"`
	Username string `json:"username"`
	URL      string `json:"url"`
}

func listCDPPages(port CDPPort) ([]cdpPage, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", int(port)))
	if err != nil {
		return nil, fmt.Errorf("cdp: list: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cdp: read: %w", err)
	}
	var pages []cdpPage
	if json.Unmarshal(raw, &pages) != nil {
		return nil, fmt.Errorf("cdp: /json/list parse failed")
	}
	return pages, nil
}

// FillCDP attaches to the agent's existing page on a headless Chrome's
// DevTools port, resolves its login form, fetches the matching credential via
// the host's Use path, and writes it into the fields. Returns the field
// count — never the credential. `urlMatch` optionally pins which page tab to
// attach (substring of its URL); `entryUUID` pins a specific entry.
func (h *Host) FillCDP(ctx context.Context, port CDPPort, urlMatch string, entryUUID string) (int, error) {
	pages, err := listCDPPages(port)
	if err != nil {
		return 0, err
	}
	var page *cdpPage
	for i := range pages {
		p := &pages[i]
		if p.Type != "page" {
			continue
		}
		if urlMatch != "" && !strings.Contains(p.URL, urlMatch) {
			continue
		}
		if p.URL == "" || p.URL == "about:blank" {
			continue
		}
		page = p
		break
	}
	if page == nil {
		return 0, fmt.Errorf("cdp: no page target found%s", map[bool]string{true: " matching " + urlMatch, false: ""}[urlMatch != ""])
	}

	browserCtx, cancelBrowser := chromedp.NewRemoteAllocator(ctx, fmt.Sprintf("http://127.0.0.1:%d", int(port)))
	defer cancelBrowser()
	bctx, bcancel := chromedp.NewContext(browserCtx)
	defer bcancel()
	if err := chromedp.Run(bctx); err != nil {
		return 0, fmt.Errorf("cdp: connect: %w", err)
	}
	pctx, pcancel := chromedp.NewContext(bctx, chromedp.WithTargetID(target.ID(page.ID)))
	defer pcancel()

	var form cdpForm
	deadline := time.Now().Add(15 * time.Second)
	for {
		var formJSON string
		if err := chromedp.Run(pctx, chromedp.Evaluate(cdpProbe, &formJSON)); err != nil {
			return 0, fmt.Errorf("cdp: probe: %w", err)
		}
		if json.Unmarshal([]byte(formJSON), &form) == nil &&
			(form.Password != "" || form.Username != "") &&
			form.URL != "" && form.URL != "about:blank" {
			break
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("cdp: no fillable form on %s", page.URL)
		}
		time.Sleep(200 * time.Millisecond)
	}
	pageURL := form.URL
	if pageURL == "" {
		pageURL = page.URL
	}

	reply := h.logins(pageURL)
	if reply.Success != "true" || len(reply.Entries) == 0 {
		return 0, fmt.Errorf("cdp: no credential matches %s", pageURL)
	}
	entry := reply.Entries[0]
	if entryUUID != "" {
		found := false
		for _, e := range reply.Entries {
			if e.UUID == entryUUID {
				entry = e
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("cdp: no entry with uuid %s", entryUUID)
		}
	}

	filled := 0
	if form.Username != "" {
		if err := chromedp.Run(pctx,
			chromedp.Focus(form.Username),
			chromedp.SendKeys(form.Username, entry.Login),
		); err != nil {
			return filled, fmt.Errorf("cdp: username write: %w", err)
		}
		filled++
	}
	if form.Password != "" {
		if err := chromedp.Run(pctx,
			chromedp.Focus(form.Password),
			chromedp.SendKeys(form.Password, entry.Password),
		); err != nil {
			return filled, fmt.Errorf("cdp: password write: %w", err)
		}
		filled++
	}
	return filled, nil
}
