package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode/utf16"
)

// CommandDigestAlgorithm names the immutable canonicalization and domain prefix.
const CommandDigestAlgorithm = "lerna-command-digest-1"

// CommandDigest binds a format-valid original command to a trusted identity.
// The host supplies subjectJSON from authenticated context, never from payload.
// Success proves neither method support nor authorization or durable acceptance.
func CommandDigest(commandJSON, subjectJSON []byte) (string, error) {
	if _, err := ParseCommand(commandJSON); err != nil {
		return "", err
	}
	if _, err := Decode[SubjectBinding](subjectJSON); err != nil {
		return "", refusal("schema_invalid", err)
	}
	parsed, err := ParseJSON(commandJSON)
	if err != nil {
		return "", refusal("schema_invalid", err)
	}
	subject, err := ParseJSON(subjectJSON)
	if err != nil {
		return "", refusal("schema_invalid", err)
	}
	command := parsed.(map[string]any)
	content := make(map[string]any)
	for _, key := range []string{"contract_version", "profile", "method", "target", "payload", "accept_before", "expected_revision"} {
		if value, present := command[key]; present {
			content[key] = value
		}
	}
	content["subject_binding"] = subject
	var canonical strings.Builder
	// This assembled hash preimage is not a new wire body. Both raw inputs have
	// already met the wire limit; adding the identity cannot invalidate that body.
	if err := writeCanonical(&canonical, content); err != nil {
		return "", refusal("schema_invalid", err)
	}
	digest := sha256.Sum256([]byte(CommandDigestAlgorithm + "\n" + canonical.String()))
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// writeCanonical implements the RFC 8785 Unicode-scalar, no-number JSON subset.
func writeCanonical(out *strings.Builder, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		writeCanonicalString(out, v)
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := writeCanonical(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			left, right := utf16.Encode([]rune(keys[i])), utf16.Encode([]rune(keys[j]))
			for k := 0; k < len(left) && k < len(right); k++ {
				if left[k] != right[k] {
					return left[k] < right[k]
				}
			}
			return len(left) < len(right)
		})
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			writeCanonicalString(out, key)
			out.WriteByte(':')
			if err := writeCanonical(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		return fmt.Errorf("non-JSON value in canonical input")
	}
	return nil
}

func writeCanonicalString(out *strings.Builder, text string) {
	const hexDigits = "0123456789abcdef"
	out.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				out.WriteString(`\u00`)
				out.WriteByte(hexDigits[r>>4])
				out.WriteByte(hexDigits[r&15])
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
