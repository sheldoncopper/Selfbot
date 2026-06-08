package variables

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/selfbot/selfbot/internal/storage"
)

// DecodeConfig parses a variable's JSON config blob into a map.
func DecodeConfig(s string) map[string]any {
	m := map[string]any{}
	if strings.TrimSpace(s) == "" {
		return m
	}
	_ = json.Unmarshal([]byte(s), &m)
	return m
}

// EncodeConfig serializes a config map to JSON.
func EncodeConfig(m map[string]any) string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// ResolveValue computes the current display value of a variable (font applied)
// and the next cursor. For custom variables it returns the literal text.
func ResolveValue(v *storage.Variable, now time.Time) (string, int) {
	cfg := DecodeConfig(v.Config)
	var raw string
	cursor := v.Cursor
	if v.Type == CustomType {
		raw, _ = cfg["text"].(string)
	} else if spec, ok := SpecByKey(v.Type); ok {
		raw, cursor = spec.compute(cfg, v.Cursor, now)
	} else {
		raw = ""
	}
	return Apply(v.Font, raw), cursor
}

// isNameRune reports whether r is allowed inside a {variable} token.
func isNameRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// RenderField renders a field template: variable tokens ({name}) are replaced
// by the cached display value of the matching variable, while the literal text
// between tokens gets the field's own font applied. Unknown tokens are left
// verbatim.
func RenderField(f *storage.Field, byName map[string]*storage.Variable) string {
	tpl := f.Template
	var out strings.Builder
	var lit strings.Builder

	flush := func() {
		if lit.Len() > 0 {
			out.WriteString(Apply(f.Font, lit.String()))
			lit.Reset()
		}
	}

	runes := []rune(tpl)
	for i := 0; i < len(runes); {
		if runes[i] == '{' {
			// Try to read a token name up to the next '}'.
			j := i + 1
			for j < len(runes) && isNameRune(runes[j]) {
				j++
			}
			if j < len(runes) && runes[j] == '}' && j > i+1 {
				name := string(runes[i+1 : j])
				if v, ok := byName[name]; ok {
					flush()
					out.WriteString(v.LastValue)
					i = j + 1
					continue
				}
			}
		}
		lit.WriteRune(runes[i])
		i++
	}
	flush()
	return out.String()
}

// TypeName returns the localized display name of a variable's type.
func TypeName(typ, lang string) string {
	if typ == CustomType {
		if lang == "en" {
			return "Custom text"
		}
		return "متن سفارشی"
	}
	if s, ok := SpecByKey(typ); ok {
		if lang == "en" {
			return s.EN
		}
		return s.FA
	}
	return typ
}
