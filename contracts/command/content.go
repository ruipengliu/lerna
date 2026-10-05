package command

import (
	"encoding/base64"
	"unicode/utf8"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// ContentBytes 统一确认展示与物理发送的字节选择，显式空 RawBody 也优先于 Text。
func ContentBytes(c *v1.Content) []byte {
	if c.GetRawBody() != nil {
		return c.RawBody
	}
	return []byte(c.GetText())
}

// ParameterDescription 无损展示参数字节及其媒体类型。
type ParameterDescription struct {
	Encoding  string
	Value     string
	MediaType string
}

// DescribeParameters 按 UTF-8 或 Base64 无损展示实际发送的参数。
func DescribeParameters(c *v1.Content) ParameterDescription {
	payload := ContentBytes(c)
	result := ParameterDescription{Encoding: "UTF-8", Value: string(payload), MediaType: c.GetMediaType()}
	if !utf8.Valid(payload) {
		result.Encoding = "BASE64"
		result.Value = base64.StdEncoding.EncodeToString(payload)
	}
	return result
}
