package execution

import (
	"encoding/json"

	"github.com/ruipengliu/lerna/api"
)

// Record 的 JSON canonical 会重排嵌套 RawMessage；原目标输入必须另存原字节，
// 用 base64 字符串跨持久化保留。不能重新编码或改写原 RequestDigest。
func restorePreparedBytes(a *Attempt) error {
	encoded := a.PreparedBytes
	if len(encoded) == 0 {
		// 旧记录只有在留下的原字节仍匹配已固定摘要时才可继续。
		encoded = a.Prepared.Encoded
	}
	if len(encoded) > api.MaxJSONBytes || api.Hash(encoded) != a.Prepared.Digest {
		// 不猜测旧未知出口的原编码，不制造未发送、重做 Prepare 或新 Attempt。
		return api.E("effect_unknown", "original_prepared_bytes_unavailable")
	}
	a.Prepared.Encoded = append(json.RawMessage{}, encoded...)
	return nil
}
