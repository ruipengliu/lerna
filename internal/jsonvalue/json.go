// Package jsonvalue implements the bounded JSON representation of the contract.
package jsonvalue

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const MaxBytes = 1 << 20

func Decode(data []byte) (any, error) {
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return nil, fmt.Errorf("JSON size or UTF-8 violation")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	v, err := read(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return v, nil
}

func read(d *json.Decoder, depth int) (any, error) {
	if depth > 64 {
		return nil, fmt.Errorf("JSON nesting exceeds 64")
	}
	token, err := d.Token()
	if err != nil {
		return nil, err
	}
	switch v := token.(type) {
	case json.Delim:
		switch v {
		case '{':
			obj := make(map[string]any)
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, fmt.Errorf("invalid object key")
				}
				if _, exists := obj[name]; exists {
					return nil, fmt.Errorf("duplicate JSON key")
				}
				obj[name], err = read(d, depth+1)
				if err != nil {
					return nil, err
				}
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return obj, nil
		case '[':
			arr := []any{}
			for d.More() {
				item, err := read(d, depth+1)
				if err != nil {
					return nil, err
				}
				arr = append(arr, item)
			}
			if _, err := d.Token(); err != nil {
				return nil, err
			}
			return arr, nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter")
		}
	case json.Number:
		// Bound exponent and token length before allocating an exact rational.
		s := v.String()
		if len(s) > 128 {
			return nil, fmt.Errorf("JSON number exceeds 128 characters")
		}
		if i := strings.IndexAny(s, "eE"); i >= 0 {
			e, err := strconv.Atoi(s[i+1:])
			if err != nil || e < -308 || e > 308 {
				return nil, fmt.Errorf("JSON exponent outside [-308,308]")
			}
		}
		n, ok := new(big.Rat).SetString(s)
		if !ok {
			return nil, fmt.Errorf("invalid JSON number")
		}
		if n.IsInt() && new(big.Int).Abs(n.Num()).Cmp(big.NewInt(9007199254740991)) > 0 {
			return nil, fmt.Errorf("precision-sensitive integer must be a decimal string")
		}
	}
	return token, nil
}

// Equal compares parsed JSON by semantic values, including exact numeric value.
func Equal(a, b any) bool {
	switch a := a.(type) {
	case json.Number:
		b, ok := b.(json.Number)
		if !ok {
			return false
		}
		x, _ := new(big.Rat).SetString(string(a))
		y, _ := new(big.Rat).SetString(string(b))
		return x.Cmp(y) == 0
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for key, value := range a {
			other, exists := b[key]
			if !exists || !Equal(value, other) {
				return false
			}
		}
		return true
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !Equal(a[i], b[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
