// Package protobuf 保存通过公共接口取得的 H1 实对象样本。
package protobuf

import (
	"encoding/json"
	"fmt"
	"os"

	_ "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

// Sample 保留样本的公共查询来源与生成绑定对象。
type Sample struct {
	Name    string
	Message proto.Message
}
type record struct {
	Name    string          `json:"name"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Save 仅供显式的样本采集使用；普通测试不改写已保存样本。
func Save(path string, samples []Sample) error {
	records := make([]record, 0, len(samples))
	for _, s := range samples {
		payload, err := protojson.Marshal(s.Message)
		if err != nil {
			return err
		}
		records = append(records, record{s.Name, string(s.Message.ProtoReflect().Descriptor().FullName()), payload})
	}
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

// Load 必须找到真实生成类型；拒绝未知字段，不以动态消息替代缺失绑定。
func Load(path string) ([]Sample, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []record
	if err = json.Unmarshal(b, &records); err != nil {
		return nil, err
	}
	result := make([]Sample, 0, len(records))
	names := map[string]bool{}
	for _, r := range records {
		if r.Name == "" || names[r.Name] {
			return nil, fmt.Errorf("empty or duplicate sample: %q", r.Name)
		}
		names[r.Name] = true
		typ, err := protoregistry.GlobalTypes.FindMessageByName(protoreflect.FullName(r.Type))
		if err != nil {
			return nil, err
		}
		m := typ.New().Interface()
		if err = protojson.Unmarshal(r.Payload, m); err != nil {
			return nil, fmt.Errorf("%s: %w", r.Name, err)
		}
		result = append(result, Sample{r.Name, m})
	}
	return result, nil
}
