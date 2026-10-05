// Package fingerprint 计算命令指纹（核心契约 3.1、3.3）。
//
// 指纹基于带版本号的语义投影：按字段编号逐个列出已设置的字段，
// 用规范编码写入摘要，而不是使用 Protobuf 的序列化字节。
// Protobuf 的确定性序列化不是规范化序列化，Schema、构建或运行库变化都可能改变字节。
package fingerprint

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Version 是当前的指纹版本。升级后不重算旧记录。
const Version = "fp1"

// ErrUnknownFields 表示正文含有不理解的字段。不理解的字段必须拒绝，不得先忽略再计算指纹。
var ErrUnknownFields = errors.New("fingerprint: message carries unknown fields")

// Of 计算命令类型 kind 与正文 m 的语义指纹。
func Of(kind string, m proto.Message) ([]byte, error) {
	var buf bytes.Buffer
	writeString(&buf, Version)
	writeString(&buf, kind)
	if m != nil {
		if err := writeMessage(&buf, m.ProtoReflect()); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256(buf.Bytes())
	return sum[:], nil
}

// MustOf 与 Of 相同，出错时 panic；只用于核心自己构造、不含未知字段的正文。
func MustOf(kind string, m proto.Message) []byte {
	fp, err := Of(kind, m)
	if err != nil {
		panic(err)
	}
	return fp
}

// Equal 比较两个指纹。
func Equal(a, b []byte) bool { return bytes.Equal(a, b) }

func writeMessage(buf *bytes.Buffer, m protoreflect.Message) error {
	if len(m.GetUnknown()) > 0 {
		return ErrUnknownFields
	}
	type entry struct {
		fd protoreflect.FieldDescriptor
		v  protoreflect.Value
	}
	var set []entry
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		set = append(set, entry{fd, v})
		return true
	})
	sort.Slice(set, func(i, j int) bool { return set[i].fd.Number() < set[j].fd.Number() })
	writeUvarint(buf, uint64(len(set)))
	for _, e := range set {
		writeUvarint(buf, uint64(e.fd.Number()))
		switch {
		case e.fd.IsList():
			l := e.v.List()
			writeUvarint(buf, uint64(l.Len()))
			for i := 0; i < l.Len(); i++ {
				if err := writeValue(buf, e.fd, l.Get(i)); err != nil {
					return err
				}
			}
		case e.fd.IsMap():
			mp := e.v.Map()
			type kv struct {
				k []byte
				v protoreflect.Value
			}
			var kvs []kv
			var err error
			mp.Range(func(k protoreflect.MapKey, v protoreflect.Value) bool {
				var kb bytes.Buffer
				if err = writeValue(&kb, e.fd.MapKey(), k.Value()); err != nil {
					return false
				}
				kvs = append(kvs, kv{kb.Bytes(), v})
				return true
			})
			if err != nil {
				return err
			}
			sort.Slice(kvs, func(i, j int) bool { return bytes.Compare(kvs[i].k, kvs[j].k) < 0 })
			writeUvarint(buf, uint64(len(kvs)))
			for _, p := range kvs {
				buf.Write(p.k)
				if err := writeValue(buf, e.fd.MapValue(), p.v); err != nil {
					return err
				}
			}
		default:
			if err := writeValue(buf, e.fd, e.v); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeValue(buf *bytes.Buffer, fd protoreflect.FieldDescriptor, v protoreflect.Value) error {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		if v.Bool() {
			buf.WriteByte(1)
		} else {
			buf.WriteByte(0)
		}
	case protoreflect.EnumKind:
		writeVarint(buf, int64(v.Enum()))
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		writeVarint(buf, v.Int())
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		writeUvarint(buf, v.Uint())
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		writeUvarint(buf, math.Float64bits(v.Float()))
	case protoreflect.StringKind:
		writeString(buf, v.String())
	case protoreflect.BytesKind:
		writeUvarint(buf, uint64(len(v.Bytes())))
		buf.Write(v.Bytes())
	case protoreflect.MessageKind, protoreflect.GroupKind:
		var inner bytes.Buffer
		if err := writeMessage(&inner, v.Message()); err != nil {
			return err
		}
		writeUvarint(buf, uint64(inner.Len()))
		buf.Write(inner.Bytes())
	default:
		return fmt.Errorf("fingerprint: unsupported field kind %v", fd.Kind())
	}
	return nil
}

func writeString(buf *bytes.Buffer, s string) {
	writeUvarint(buf, uint64(len(s)))
	buf.WriteString(s)
}

func writeUvarint(buf *bytes.Buffer, x uint64) {
	var b [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(b[:], x)
	buf.Write(b[:n])
}

func writeVarint(buf *bytes.Buffer, x int64) {
	var b [binary.MaxVarintLen64]byte
	n := binary.PutVarint(b[:], x)
	buf.Write(b[:n])
}
