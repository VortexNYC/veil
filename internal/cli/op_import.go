package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/VortexNYC/veil/internal/oneimport"
)

// dumpFromOPCLI streams items from the local signed-in 1Password CLI (`op`)
// into an in-memory oneimport.OPDump JSON payload. Stdout from `op item get`
// carries secrets and is never logged or echoed into errors.
func dumpFromOPCLI(ctx context.Context, vault string, progress io.Writer) ([]byte, error) {
	if _, err := exec.LookPath("op"); err != nil {
		return nil, fmt.Errorf("import --from-op: 1Password CLI (op) not found on PATH")
	}
	listArgs := []string{"item", "list", "--format", "json"}
	if v := strings.TrimSpace(vault); v != "" {
		listArgs = append(listArgs, "--vault", v)
	}
	listCmd := exec.CommandContext(ctx, "op", listArgs...)
	listOut, err := listCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("import --from-op: op item list failed (sign in to op first)")
	}
	var overviews []map[string]any
	if err := json.Unmarshal(listOut, &overviews); err != nil {
		return nil, fmt.Errorf("import --from-op: invalid op item list output")
	}
	var filtered []map[string]any
	for _, ov := range overviews {
		cat, _ := ov["category"].(string)
		switch strings.ToUpper(strings.TrimSpace(cat)) {
		case "LOGIN", "PASSWORD", "API_CREDENTIAL", "DATABASE", "SERVER", "CREDIT_CARD":
			filtered = append(filtered, ov)
		}
	}
	if len(filtered) == 0 {
		return json.Marshal(oneimport.OPDump{Version: "1", Items: []oneimport.OPItem{}})
	}
	if progress != nil {
		_, _ = fmt.Fprintf(progress, "veil: fetching %d items from 1Password CLI...\n", len(filtered))
	}
	pipedInput, err := json.Marshal(filtered)
	if err != nil {
		return nil, err
	}
	getCmd := exec.CommandContext(ctx, "op", "item", "get", "-", "--reveal", "--format", "json")
	getCmd.Stdin = bytes.NewReader(pipedInput)
	getOut, err := getCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("import --from-op: op item get failed")
	}
	dec := json.NewDecoder(bytes.NewReader(getOut))
	var items []oneimport.OPItem
	for {
		var it oneimport.OPItem
		if err := dec.Decode(&it); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("import --from-op: failed decoding op item stream")
		}
		items = append(items, it)
	}
	return json.Marshal(oneimport.OPDump{
		Version: "1",
		Items:   items,
	})
}
