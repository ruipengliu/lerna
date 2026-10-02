package durable

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// Intent is a strict envelope for an already method-validated command.
// Full method input, target correlation and authorization remain entry ports.
type Intent struct {
	CommandID        string          `json:"command_id"`
	Method           string          `json:"method"`
	TargetID         string          `json:"target_id"`
	ExpiresAt        string          `json:"expires_at"`
	ExpectedRevision *int64          `json:"expected_revision,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}
type FixedIntent struct {
	intent  Intent
	digest  string
	expires int64
}

func (f FixedIntent) CommandID() string { return f.intent.CommandID }
func (f FixedIntent) Method() string    { return f.intent.Method }
func (f FixedIntent) TargetID() string  { return f.intent.TargetID }
func (f FixedIntent) Digest() string    { return f.digest }

// Value returns a copy for domain persistence; changing it cannot change intent.
func (f FixedIntent) Value() Intent {
	v := f.intent
	v.Payload = append(json.RawMessage(nil), v.Payload...)
	if v.ExpectedRevision != nil {
		n := *v.ExpectedRevision
		v.ExpectedRevision = &n
	}
	return v
}

// CanonicalJSON checks raw syntax/Unicode/duplicates and safe integer values
// before JCS can round numbers. It is not a replacement for method Schema.
func CanonicalJSON(raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrPrecondition
	}
	d := jsontext.NewDecoder(bytes.NewReader(raw), jsontext.AllowDuplicateNames(false), jsontext.AllowInvalidUTF8(false))
	for {
		t, err := d.ReadToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if t.Kind() == '0' {
			if !safeIntegerToken(t.String()) {
				return nil, ErrPrecondition
			}
			v, err := t.Float()
			if err != nil || math.IsInf(v, 0) || math.IsNaN(v) {
				return nil, ErrPrecondition
			}
		}
	}
	v := jsontext.Value(append([]byte(nil), raw...))
	if err := v.Canonicalize(jsontext.AllowDuplicateNames(false), jsontext.AllowInvalidUTF8(false)); err != nil {
		return nil, err
	}
	return []byte(v), nil
}

// Inspect decimal digits without allocating a power for an untrusted exponent.
// Fractional values remain available as raw input to method validators.
func safeIntegerToken(s string) bool {
	s = strings.TrimPrefix(s, "-")
	exponent := int64(0)
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		rawExponent := s[i+1:]
		s = s[:i]
		var err error
		exponent, err = strconv.ParseInt(rawExponent, 10, 32)
		if err != nil {
			if strings.Trim(s, "0.") == "" {
				return true
			}
			return strings.HasPrefix(rawExponent, "-")
		}
	}
	frac := 0
	if i := strings.IndexByte(s, '.'); i >= 0 {
		frac = len(s) - i - 1
		s = s[:i] + s[i+1:]
	}
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return true
	}
	shift := exponent - int64(frac)
	if shift < 0 {
		if -shift >= int64(len(s)) {
			return true
		}
		n := len(s) + int(shift)
		if strings.Trim(s[n:], "0") != "" {
			return true
		}
		s = s[:n]
		shift = 0
	}
	const bound = "9007199254740991"
	length := int64(len(s)) + shift
	if length != int64(len(bound)) {
		return length < int64(len(bound))
	}
	return s+strings.Repeat("0", int(shift)) <= bound
}
func FixIntent(i Intent) (FixedIntent, error) {
	if !idPattern.MatchString(i.CommandID) || !validName(i.Method) || !validName(i.TargetID) || !strings.HasSuffix(i.ExpiresAt, "Z") {
		return FixedIntent{}, ErrPrecondition
	}
	expires, err := time.Parse(time.RFC3339Nano, i.ExpiresAt)
	if err != nil {
		return FixedIntent{}, err
	}
	if i.ExpectedRevision != nil && (*i.ExpectedRevision < 0 || *i.ExpectedRevision > 9007199254740991) {
		return FixedIntent{}, ErrPrecondition
	}
	payload, err := CanonicalJSON(i.Payload)
	if err != nil || len(payload) == 0 || payload[0] != '{' {
		return FixedIntent{}, ErrPrecondition
	}
	// Keep original number spelling for Schema/domain checks before any rounding.
	i.Payload = append(json.RawMessage(nil), i.Payload...)
	if i.ExpectedRevision != nil {
		n := *i.ExpectedRevision
		i.ExpectedRevision = &n
	}
	raw, err := json.Marshal(i)
	if err != nil {
		return FixedIntent{}, err
	}
	canonical, err := CanonicalJSON(raw)
	if err != nil {
		return FixedIntent{}, err
	}
	sum := sha256.Sum256(canonical)
	return FixedIntent{i, hex.EncodeToString(sum[:]), expires.UnixMilli()}, nil
}
