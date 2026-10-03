package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

const MaxBodyBytes = 1 << 20
const MaxDepth = 64

// ParseJSON is the strict raw-wire boundary. Version 1 has no JSON number values;
// exact quantities are strings. It preserves every valid Unicode scalar.
func ParseJSON(data []byte) (any, error) {
	if len(data) > MaxBodyBytes {
		return nil, fmt.Errorf("body exceeds %d bytes", MaxBodyBytes)
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("invalid UTF-8")
	}
	if err := validateEscapedSurrogates(data); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := parseValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return value, nil
}

func parseValue(decoder *json.Decoder, depth int) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token := token.(type) {
	case json.Number:
		return nil, fmt.Errorf("JSON numbers are not supported; use exact decimal strings")
	case json.Delim:
		depth++
		if depth > MaxDepth {
			return nil, fmt.Errorf("container depth exceeds %d", MaxDepth)
		}
		switch token {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, exists := object[name]; exists {
					return nil, fmt.Errorf("duplicate key %q", name)
				}
				value, err := parseValue(decoder, depth)
				if err != nil {
					return nil, err
				}
				object[name] = value
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return nil, fmt.Errorf("invalid object end")
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for decoder.More() {
				value, err := parseValue(decoder, depth)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return nil, fmt.Errorf("invalid array end")
			}
			return array, nil
		}
		return nil, fmt.Errorf("unexpected delimiter")
	default:
		return token, nil
	}
}

// encoding/json replaces isolated escaped surrogates. Check them lexically first.
func validateEscapedSurrogates(data []byte) error {
	inString := false
	for i := 0; i < len(data); i++ {
		if data[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return fmt.Errorf("incomplete escape")
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return fmt.Errorf("incomplete unicode escape")
		}
		point, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return fmt.Errorf("invalid unicode escape")
		}
		i += 4
		if point >= 0xDC00 && point <= 0xDFFF {
			return fmt.Errorf("isolated low surrogate")
		}
		if point < 0xD800 || point > 0xDBFF {
			continue
		}
		if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
			return fmt.Errorf("isolated high surrogate")
		}
		low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
		if err != nil || low < 0xDC00 || low > 0xDFFF {
			return fmt.Errorf("invalid surrogate pair")
		}
		i += 6
	}
	return nil
}
