package egressio

// nativeFileEvent 记录已完成的原生操作；同步事件仅在全部原语成功后产生。
type nativeFileEvent struct {
	Sequence  int    `json:"sequence"`
	Kind      string `json:"kind"`
	Stage     string `json:"stage"`
	Directory string `json:"directory"`
	Name      string `json:"name,omitempty"`
	NewName   string `json:"new_name,omitempty"`
	Identity  string `json:"identity,omitempty"`
	Offset    int64  `json:"offset,omitempty"`
	Data      []byte `json:"data,omitempty"`
	Requested int    `json:"requested,omitempty"`
	Length    int    `json:"length,omitempty"`
	Error     string `json:"error,omitempty"`
}
