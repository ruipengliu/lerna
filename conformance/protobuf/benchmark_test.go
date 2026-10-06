package protobuf_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：R6、G12
func TestWriteProtobufMeasurementInventory(t *testing.T) {
	path := os.Getenv("LERNA_PROTOBUF_REPORT")
	if path == "" {
		t.Skip("explicit measurement output only")
	}
	samples, err := protobuf.Load("testdata/mainline.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := protobuf.Inspect(samples)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkPublicObjects(b *testing.B) {
	samples, err := protobuf.Load("testdata/mainline.json")
	if err != nil {
		b.Fatal(err)
	}
	for _, s := range samples {
		b.Run(s.Name, func(b *testing.B) {
			binary, err := proto.Marshal(s.Message)
			if err != nil {
				b.Fatal(err)
			}
			jsonWire, err := protojson.Marshal(s.Message)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("binary-encode", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(binary)))
				for b.Loop() {
					if _, err := proto.Marshal(s.Message); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("binary-decode", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(binary)))
				for b.Loop() {
					m := s.Message.ProtoReflect().Type().New().Interface()
					if err := proto.Unmarshal(binary, m); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("json-encode", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(jsonWire)))
				for b.Loop() {
					if _, err := protojson.Marshal(s.Message); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("json-decode", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(len(jsonWire)))
				for b.Loop() {
					m := s.Message.ProtoReflect().Type().New().Interface()
					if err := protojson.Unmarshal(jsonWire, m); err != nil {
						b.Fatal(err)
					}
				}
			})
			// 这里只计生产入口的结构／安全语义检查；不包括持久读取和门禁事务。
			switch m := s.Message.(type) {
			case *v1.SubmitGoalCommand:
				b.Run("semantic-goal", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if err := command.ValidateGoal(m); err != nil {
							b.Fatal(err)
						}
					}
				})
			case interface {
				GetHeader() *v1.CommandHeader
				proto.Message
			}:
				b.Run("semantic-header", func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if err := command.ValidateHeader(m.GetHeader(), m); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
