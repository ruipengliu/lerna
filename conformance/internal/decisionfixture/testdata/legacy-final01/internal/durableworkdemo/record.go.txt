// Package durablework is an internal Host demonstration, not an Application API.
package durableworkdemo

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/ruipengliu/lerna/contract"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed record.schema.json
var recordSchema []byte
var schemaOnce sync.Once
var compiledSchema *jsonschema.Schema
var schemaError error

type Record struct {
	Envelope contract.CommandEnvelope
	Text     string
}

// DecodeRecord combines the immutable common envelope rules with this Host's
// closed method schema. It never registers a public method or normalizes text.
func DecodeRecord(data []byte) (Record, error) {
	var result Record
	envelope, err := contract.ParseCommand(data)
	if err != nil {
		return result, err
	}
	schemaOnce.Do(func() {
		var raw any
		schemaError = json.Unmarshal(recordSchema, &raw)
		if schemaError != nil {
			return
		}
		compiler := jsonschema.NewCompiler()
		schemaError = compiler.AddResource("https://lerna.dev/host/durable-work-1/record.json", raw)
		if schemaError == nil {
			compiledSchema, schemaError = compiler.Compile("https://lerna.dev/host/durable-work-1/record.json")
		}
	})
	if schemaError != nil {
		return result, publicError("schema_invalid", schemaError)
	}
	raw, err := contract.ParseJSON(data)
	if err == nil {
		err = compiledSchema.Validate(raw)
	}
	if err != nil {
		return result, publicError("schema_invalid", err)
	}
	var payload struct {
		Text string `json:"text"`
	}
	if err = json.Unmarshal(envelope.Payload, &payload); err != nil {
		return result, publicError("schema_invalid", err)
	}
	return Record{Envelope: envelope, Text: payload.Text}, nil
}
func publicError(code contract.ErrorCode, cause error) error {
	return &contract.ContractError{PublicError: contract.PublicError{Code: code}, Cause: cause}
}
