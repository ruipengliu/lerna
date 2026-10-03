package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ParseJSON 在普通解码前拒绝原始重复键、非法 Unicode、不安全数值和超深输入。
func ParseJSON(raw []byte) (any, error) {
	if len(raw) == 0 || len(raw) > MaxJSONBytes || !utf8.Valid(raw) {
		return nil, E("invalid_request", "invalid_json_bytes")
	}
	if err := checkStrings(raw); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	v, err := parseValue(d, 0)
	if err != nil {
		return nil, E("invalid_request", "invalid_json")
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, E("invalid_request", "trailing_json")
	}
	return v, nil
}
func parseValue(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("depth")
	}
	t, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch x := t.(type) {
	case json.Delim:
		if x == '{' {
			m := map[string]any{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return nil, err
				}
				key, ok := k.(string)
				if !ok {
					return nil, fmt.Errorf("key")
				}
				if _, ok := m[key]; ok {
					return nil, fmt.Errorf("duplicate key")
				}
				v, err := parseValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				m[key] = v
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("object")
			}
			return m, nil
		}
		if x == '[' {
			a := []any{}
			for d.More() {
				if len(a) >= 10000 {
					return nil, fmt.Errorf("array bound")
				}
				v, err := parseValue(d, depth+1)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("array")
			}
			return a, nil
		}
		return nil, fmt.Errorf("delimiter")
	case json.Number:
		f, err := strconv.ParseFloat(string(x), 64)
		if err != nil || math.IsInf(f, 0) || math.IsNaN(f) || math.Abs(f) > float64(MaxSafeInteger) {
			return nil, fmt.Errorf("unsafe number")
		}
		return f, nil
	default:
		return x, nil
	}
}
func checkStrings(b []byte) error {
	for i := 0; i < len(b); i++ {
		if b[i] != '"' {
			continue
		}
		i++
		for ; i < len(b) && b[i] != '"'; i++ {
			if b[i] < 32 {
				return E("invalid_request", "invalid_unicode")
			}
			if b[i] != '\\' {
				continue
			}
			i++
			if i >= len(b) {
				return E("invalid_request", "invalid_unicode")
			}
			if b[i] != 'u' {
				continue
			}
			if i+4 >= len(b) {
				return E("invalid_request", "invalid_unicode")
			}
			x, err := strconv.ParseUint(string(b[i+1:i+5]), 16, 16)
			if err != nil {
				return E("invalid_request", "invalid_unicode")
			}
			i += 4
			if x >= 0xDC00 && x <= 0xDFFF {
				return E("invalid_request", "invalid_unicode")
			}
			if x >= 0xD800 && x <= 0xDBFF {
				if i+6 >= len(b) || b[i+1] != '\\' || b[i+2] != 'u' {
					return E("invalid_request", "invalid_unicode")
				}
				y, err := strconv.ParseUint(string(b[i+3:i+7]), 16, 16)
				if err != nil || y < 0xDC00 || y > 0xDFFF {
					return E("invalid_request", "invalid_unicode")
				}
				i += 6
			}
		}
		if i >= len(b) {
			return E("invalid_request", "invalid_unicode")
		}
	}
	return nil
}
func Decode(raw []byte, v any) error {
	if _, err := ParseJSON(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return E("invalid_request", "invalid_payload")
	}
	return nil
}
func Canonical(raw []byte) ([]byte, error) {
	v, err := ParseJSON(raw)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeCanonical(&b, v)
	return b.Bytes(), nil
}
func Digest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	b, err = Canonical(b)
	if err != nil {
		return "", err
	}
	return Hash(b), nil
}
func writeCanonical(b *bytes.Buffer, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		quote(b, x)
	case float64:
		b.WriteString(canonicalNumber(x))
	case []any:
		b.WriteByte('[')
		for i, y := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonical(b, y)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, c := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for n := 0; n < len(a) && n < len(c); n++ {
				if a[n] != c[n] {
					return a[n] < c[n]
				}
			}
			return len(a) < len(c)
		})
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			quote(b, k)
			b.WriteByte(':')
			writeCanonical(b, x[k])
		}
		b.WriteByte('}')
	}
}
func quote(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			if r < 32 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
func canonicalNumber(f float64) string {
	if f == 0 {
		return "0"
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	parts := strings.Split(s, "e")
	exp, _ := strconv.Atoi(parts[1])
	digits := strings.ReplaceAll(parts[0], ".", "")
	neg := strings.HasPrefix(digits, "-")
	digits = strings.TrimPrefix(digits, "-")
	sign := ""
	if neg {
		sign = "-"
	}
	if exp >= -6 && exp < 21 {
		point := exp + 1
		switch {
		case point <= 0:
			return sign + "0." + strings.Repeat("0", -point) + digits
		case point >= len(digits):
			return sign + digits + strings.Repeat("0", point-len(digits))
		default:
			return sign + digits[:point] + "." + digits[point:]
		}
	}
	plus := ""
	if exp >= 0 {
		plus = "+"
	}
	return parts[0] + "e" + plus + strconv.Itoa(exp)
}
