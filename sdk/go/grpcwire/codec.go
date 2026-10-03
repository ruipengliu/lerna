// Package grpcwire 严格验证同版 Protobuf 外壳；JSON 字节原样保留。
package grpcwire

import (
	"fmt"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const MaxFrameBytes = 1 << 20

// Codec 必须由 ForceCodec/ForceServerCodec 安装；标准 proto 解码器允许覆盖重复 oneof。
type Codec struct{}

func (Codec) Name() string { return "proto" }
func (Codec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("unsupported proto message")
	}
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(message)
	if err != nil {
		return nil, err
	}
	if err = validate(b, value); err != nil {
		return nil, err
	}
	return b, nil
}
func (Codec) Unmarshal(b []byte, value any) error {
	if err := validate(b, value); err != nil {
		return err
	}
	message, ok := value.(proto.Message)
	if !ok {
		return fmt.Errorf("unsupported proto message")
	}
	return (proto.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(b, message)
}

func validate(b []byte, value any) error {
	if len(b) == 0 || len(b) > MaxFrameBytes {
		return fmt.Errorf("proto envelope size outside bound")
	}
	var layout int
	switch value.(type) {
	case *rpcv1.CallRequest:
		layout = 1
	case *rpcv1.CallResponse:
		layout = 2
	case *rpcv1.ChannelFrame:
		layout = 3
	default:
		return fmt.Errorf("unsupported proto envelope")
	}
	seen := map[protowire.Number]bool{}
	oneof := false
	for len(b) > 0 {
		number, wire, n := protowire.ConsumeTag(b)
		if n < 0 || number < 1 || seen[number] {
			return fmt.Errorf("invalid or duplicate proto field")
		}
		b = b[n:]
		seen[number] = true
		isString, isJSON, isVarint, isOneof := false, false, false, false
		switch layout {
		case 1:
			isString = number == 1
			isJSON, isOneof = number >= 2 && number <= 5, number >= 2 && number <= 5
		case 2:
			isJSON, isOneof = number >= 1 && number <= 4, number >= 1 && number <= 4
		case 3:
			isString, isVarint, isJSON = number == 1, number == 2, number == 3
		}
		if !isString && !isJSON && !isVarint {
			return fmt.Errorf("unknown proto field")
		}
		if isOneof {
			if oneof {
				return fmt.Errorf("duplicate proto oneof")
			}
			oneof = true
		}
		if isVarint {
			if wire != protowire.VarintType {
				return fmt.Errorf("invalid proto field wire type")
			}
			v, consumed := protowire.ConsumeVarint(b)
			if consumed < 0 || v == 0 || v > api.MaxSafeInteger {
				return fmt.Errorf("invalid binding revision")
			}
			b = b[consumed:]
			continue
		}
		if wire != protowire.BytesType {
			return fmt.Errorf("invalid proto field wire type")
		}
		body, consumed := protowire.ConsumeBytes(b)
		if consumed < 0 || len(body) == 0 {
			return fmt.Errorf("invalid proto bytes field")
		}
		b = b[consumed:]
		if isString {
			if !utf8.Valid(body) || !api.ValidID(string(body)) {
				return fmt.Errorf("invalid proto identifier")
			}
		} else if isJSON {
			limit := api.MaxJSONBytes
			if layout == 3 {
				limit = MaxFrameBytes
			}
			if _, err := api.ParseJSONLimit(body, limit); err != nil {
				return fmt.Errorf("invalid inner JSON: %w", err)
			}
		}
	}
	if layout == 1 && (!seen[1] || !oneof) || layout == 2 && !oneof || layout == 3 && (!seen[1] || !seen[2] || !seen[3]) {
		return fmt.Errorf("required proto field missing")
	}
	return nil
}
