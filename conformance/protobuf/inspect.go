package protobuf

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Report 区分生成绑定覆盖与真实对象覆盖；后者不能由空对象补齐。
type Report struct {
	SchemaSHA256      string   `json:"schema_sha256"`
	GeneratedBindings int      `json:"generated_bindings"`
	SampledTypes      int      `json:"sampled_types"`
	UnsampledTypes    []string `json:"unsampled_types"`
	PopulatedFields   []string `json:"populated_fields"`
	Samples           []Size   `json:"samples"`
}
type Size struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	BinaryBytes int    `json:"binary_bytes"`
	JSONBytes   int    `json:"json_bytes"`
}

// Inspect 核验所有 lerna.v1 生成绑定，同时递归统计实际样本出现的类型和字段。
func Inspect(samples []Sample) (*Report, error) {
	report := &Report{}
	files := &descriptorpb.FileDescriptorSet{}
	protoregistry.GlobalFiles.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		if f.Package() == "lerna.v1" {
			files.File = append(files.File, protodesc.ToFileDescriptorProto(f))
		}
		return true
	})
	sort.Slice(files.File, func(i, j int) bool { return files.File[i].GetName() < files.File[j].GetName() })
	wire, err := proto.MarshalOptions{Deterministic: true}.Marshal(files)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(wire)
	report.SchemaSHA256 = hex.EncodeToString(digest[:])
	seen, fields := map[string]bool{}, map[string]bool{}
	var visit func(protoreflect.Message)
	visit = func(m protoreflect.Message) {
		seen[string(m.Descriptor().FullName())] = true
		m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			fields[string(f.FullName())] = true
			if f.IsMap() {
				if f.MapValue().Message() != nil {
					v.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool { visit(v.Message()); return true })
				}
			} else if f.Message() != nil {
				if f.IsList() {
					for i := 0; i < v.List().Len(); i++ {
						visit(v.List().Get(i).Message())
					}
				} else {
					visit(v.Message())
				}
			}
			return true
		})
	}
	for _, s := range samples {
		visit(s.Message.ProtoReflect())
		b, err := proto.Marshal(s.Message)
		if err != nil {
			return nil, err
		}
		j, err := protojson.Marshal(s.Message)
		if err != nil {
			return nil, err
		}
		report.Samples = append(report.Samples, Size{s.Name, string(s.Message.ProtoReflect().Descriptor().FullName()), len(b), len(j)})
	}
	var bindingErr error
	protoregistry.GlobalFiles.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		if f.Package() != "lerna.v1" {
			return true
		}
		var check func(protoreflect.MessageDescriptors)
		check = func(messages protoreflect.MessageDescriptors) {
			for i := 0; i < messages.Len(); i++ {
				d := messages.Get(i)
				if !d.IsMapEntry() {
					typ, err := protoregistry.GlobalTypes.FindMessageByName(d.FullName())
					if err != nil {
						bindingErr = err
						return
					}
					if typ.Descriptor() != d {
						bindingErr = fmt.Errorf("descriptor mismatch: %s", d.FullName())
						return
					}
					report.GeneratedBindings++
					if !seen[string(d.FullName())] {
						report.UnsampledTypes = append(report.UnsampledTypes, string(d.FullName()))
					}
				}
				check(d.Messages())
			}
		}
		check(f.Messages())
		return bindingErr == nil
	})
	if bindingErr != nil {
		return nil, bindingErr
	}
	report.SampledTypes = len(seen)
	for f := range fields {
		report.PopulatedFields = append(report.PopulatedFields, f)
	}
	sort.Strings(report.PopulatedFields)
	sort.Strings(report.UnsampledTypes)
	return report, nil
}
