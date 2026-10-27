// Package inject rewrites a template using granted item names.
//
// Infisical placeholder shape: ${NAME}. veil://name is the same lookup.
// Resolution happens inside the broker for a child file. The expanded
// bytes are not an agent-visible type.
package inject

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/VortexNYC/veil/internal/broker"
)

var ref = regexp.MustCompile(`\$\{([A-Za-z0-9_-]+)\}|veil://([A-Za-z0-9_-]+)|veil\(["']?([A-Za-z0-9_-]+)["']?\)`)

// Map is EnvName(item) -> value from ChildEnv pairs.
func Map(pairs []string) map[string]string {
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// Expand replaces ${ITEM}, veil://item, and veil(item). Unknown refs fail closed.
func Expand(src []byte, granted map[string]string) ([]byte, error) {
	var first error
	out := ref.ReplaceAllFunc(src, func(raw []byte) []byte {
		m := ref.FindSubmatch(raw)
		name := ""
		for i := 1; i < len(m); i++ {
			if len(m[i]) > 0 {
				name = string(m[i])
				break
			}
		}
		key := broker.EnvName(name)
		v, ok := granted[key]
		if !ok {
			if first == nil {
				first = fmt.Errorf("inject: unknown ref %s", name)
			}
			return raw
		}
		return []byte(v)
	})
	if first != nil {
		return nil, first
	}
	return out, nil
}

// ExpandEnvSchema parses a declarative .env.schema file (supporting @env-spec
// comment headers and veil://item, veil(item), or ${ITEM} references) and
// returns resolved KEY=VALUE pairs.
func ExpandEnvSchema(src []byte, granted map[string]string) ([]string, error) {
	var out []string
	for line := range strings.Lines(string(src)) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "export ")
		k, v, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" {
			continue
		}
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		} else if idx := strings.Index(v, " #"); idx >= 0 {
			v = strings.TrimSpace(v[:idx])
		}
		expanded, err := Expand([]byte(v), granted)
		if err != nil {
			return nil, err
		}
		out = append(out, k+"="+string(expanded))
	}
	return out, nil
}
