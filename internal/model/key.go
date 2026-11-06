package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// NormText trims, collapses whitespace and lower-cases s; used for identity comparisons.
func NormText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// normTS renders an instant in UTC in Python's isoformat() style (microsecond precision,
// "+00:00" suffix), or nil for a missing timestamp.
func normTS(t *time.Time) any {
	if t == nil {
		return nil
	}
	u := t.UTC()
	out := u.Format("2006-01-02T15:04:05")
	if micro := u.Nanosecond() / 1000; micro != 0 {
		out += fmt.Sprintf(".%06d", micro)
	}
	return out + "+00:00"
}

// hashKey hashes the canonical JSON of fields: keys sorted, no spaces, ASCII-only strings.
func hashKey(fields map[string]any) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsonString(k))
		b.WriteByte(':')
		switch v := fields[k].(type) {
		case nil:
			b.WriteString("null")
		case string:
			b.WriteString(jsonString(v))
		case bool:
			if v {
				b.WriteString("true")
			} else {
				b.WriteString("false")
			}
		default:
			panic(fmt.Sprintf("unsupported key field type %T", v))
		}
	}
	b.WriteByte('}')
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// jsonString encodes s like Python's json.dumps(ensure_ascii=True).
func jsonString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == '\b':
			b.WriteString(`\b`)
		case r == '\f':
			b.WriteString(`\f`)
		case r < 0x20:
			fmt.Fprintf(&b, `\u%04x`, r)
		case r < 0x80:
			b.WriteRune(r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			r -= 0x10000
			fmt.Fprintf(&b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
		}
	}
	b.WriteByte('"')
	return b.String()
}
