package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	contract "github.com/ruipengliu/lerna/contract/v1_1"
)

const maxFrameBytes = 8 * 1024 * 1024

// Private IPC carries raw bytes; product validation remains in typed run.
func batch() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), maxFrameBytes+2)
	scanner.Split(func(data []byte, atEOF bool) (int, []byte, error) {
		if atEOF && len(data) > 0 && bytes.IndexByte(data, '\n') < 0 {
			return 0, nil, fmt.Errorf("incomplete request frame")
		}
		return bufio.ScanLines(data, atEOF)
	})
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > maxFrameBytes {
			return fmt.Errorf("request frame exceeds 8 MiB")
		}
		if !utf8.Valid(line) {
			return fmt.Errorf("invalid request frame UTF-8")
		}
		// encoding/json struct matching ignores field case. The private frame
		// requires these exact keys, matching the TypeScript runner.
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(line, &fields); err != nil {
			return err
		}
		if len(fields) != 3 || fields["id"] == nil || fields["schema"] == nil || fields["wire_base64"] == nil {
			return fmt.Errorf("invalid request frame shape")
		}
		var request struct {
			ID         string  `json:"id"`
			Schema     string  `json:"schema"`
			WireBase64 *string `json:"wire_base64"`
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return err
		}
		if decoder.Decode(new(any)) != io.EOF {
			return fmt.Errorf("trailing request frame data")
		}
		if request.WireBase64 == nil || len(request.ID) == 0 || len(request.ID) > 128 || len(request.Schema) == 0 || len(request.Schema) > 128 {
			return fmt.Errorf("invalid request identity")
		}
		wire, err := base64.StdEncoding.DecodeString(*request.WireBase64)
		if err != nil || base64.StdEncoding.EncodeToString(wire) != *request.WireBase64 {
			return fmt.Errorf("invalid request base64")
		}
		result, err := run(request.Schema, wire)
		response := struct {
			ID         string                `json:"id"`
			OK         bool                  `json:"ok"`
			WireBase64 string                `json:"wire_base64,omitempty"`
			Error      *contract.PublicError `json:"error,omitempty"`
		}{ID: request.ID, OK: err == nil}
		if err != nil {
			refusal := publicError(err)
			response.Error = &refusal
		} else {
			response.WireBase64 = base64.StdEncoding.EncodeToString(result)
		}
		if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
			return err
		}
	}
	return scanner.Err()
}
