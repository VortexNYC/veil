package fill

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/VortexNYC/veil/internal/app"
	"github.com/VortexNYC/veil/internal/protocol"
)

// fillAuditQueueDir is the durable pending-audit maildir in the vault dir.
// A replica-served fill publishes one JSONL file per disclosure — fsynced
// before the secret leaves the host — and flushFillEvents ships pending
// files to POST /v1/fill/events on the next origin contact. An event that
// survives publish is never memory-only — same durability contract the
// spool auditor gives origin-side writes.
//
// Several host processes share the vault dir (Chrome spawns a host per
// extension, the bridge is another), so the queue is a maildir, not a
// shared file: an append writes name.tmp then renames to name.jsonl —
// publish is atomic, so a flush never reads a torn write, and no open fd
// can land a line inside a segment another process already claimed.
// Delivery is at-least-once — a crash between POST and unlink replays a
// file, which beats losing it.
const fillAuditQueueDir = "fill-audit-queue"

// fillEventReport is one queued disclosure. The wire shape is also the file
// format; TS is the client's fill time, authoritative for the audit row.
type fillEventReport struct {
	UUID     string `json:"uuid"`
	Kind     string `json:"kind,omitempty"`
	MintTOTP bool   `json:"mint_totp,omitempty"`
	TS       string `json:"ts"`
}

// recordFillEvent attests a locally-served disclosure. Replica fills never
// call origin for the secret, so without this the release would leave no row
// — the publish is fail-closed for the same reason origin's audit append is.
// Local-vault mode (no origin) writes through the App auditor instead.
func (h *Host) recordFillEvent(uuid, kind string, minted bool) error {
	if h.Origin != "" {
		if err := h.appendFillEvent(fillEventReport{
			UUID: uuid, Kind: kind, MintTOTP: minted,
			TS: time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
		h.flushWg.Add(1)
		go func() {
			defer h.flushWg.Done()
			h.flushFillEvents()
		}()
		return nil
	}
	if h.App == nil {
		return nil
	}
	human := protocol.Principal{Kind: protocol.PrincipalHuman, ID: h.App.HumanID, OrgID: h.App.OrgID}
	return h.App.RecordFillEvents(human, []app.FillEventReport{{UUID: uuid, Kind: kind, MintTOTP: minted, At: time.Now().UTC()}})
}

// waitFlushes drains in-flight async flushes. Tests use it before touching
// host fields a flusher reads; the production loop never blocks on it.
func (h *Host) waitFlushes() {
	h.flushWg.Wait()
}

func (h *Host) fillAuditDir() string {
	dir := h.Dir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".veil")
	}
	return filepath.Join(dir, fillAuditQueueDir)
}

// appendFillEvent publishes one event file: write tmp, fsync, atomic rename
// to .jsonl, fsync the dir. Any failure means the event was never queued —
// the caller denies the fill.
func (h *Host) appendFillEvent(ev fillEventReport) error {
	dir := h.fillAuditDir()
	if dir == "" {
		return fmt.Errorf("fill: no vault dir for audit queue")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	var nonce [4]byte
	_, _ = rand.Read(nonce[:])
	base := fmt.Sprintf("%d-%s", time.Now().UnixNano(), hex.EncodeToString(nonce[:]))
	tmp := filepath.Join(dir, base+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, filepath.Join(dir, base+".jsonl")); err != nil {
		return err
	}
	fsyncDir(dir)
	return nil
}

func fsyncDir(dir string) {
	d, err := os.Open(dir)
	if err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
}

// flushFillEvents posts pending disclosures in arrival order, then deletes
// the files that posted. A failed POST leaves the files for the next flush.
// Files holding no valid event — torn publishes that never confirmed an
// append — are junk, not disclosures, so they are removed too.
func (h *Host) flushFillEvents() {
	if h.Origin == "" {
		return
	}
	dir := h.fillAuditDir()
	if dir == "" {
		return
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	for i := 0; i < len(files); {
		var batch []fillEventReport
		var owned []string
		for ; i < len(files) && len(batch) < 256; i++ {
			raw, err := os.ReadFile(files[i])
			if err != nil {
				continue
			}
			owned = append(owned, files[i])
			for _, ln := range bytes.Split(raw, []byte("\n")) {
				var ev fillEventReport
				if json.Unmarshal(ln, &ev) == nil && strings.TrimSpace(ev.UUID) != "" {
					batch = append(batch, ev)
				}
			}
		}
		if len(batch) > 0 {
			payload, err := json.Marshal(struct {
				Events []fillEventReport `json:"events"`
			}{Events: batch})
			if err != nil {
				return
			}
			if _, err := h.originPOST("/v1/fill/events", payload); err != nil {
				fillDebug("fill events flush " + err.Error())
				return
			}
		}
		for _, f := range owned {
			_ = os.Remove(f)
		}
		fillDebug(fmt.Sprintf("fill events flushed n=%d", len(batch)))
	}
	// Reap abandoned tmp writes — a crash between create and publish leaves
	// one. Only files old enough that no writer can still hold them open.
	stale, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	cutoff := time.Now().Add(-10 * time.Minute)
	for _, tmp := range stale {
		if st, err := os.Stat(tmp); err == nil && st.ModTime().Before(cutoff) {
			_ = os.Remove(tmp)
		}
	}
}
