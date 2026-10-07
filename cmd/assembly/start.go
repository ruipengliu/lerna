package assembly

import "context"

// start 只在完整装配后委托固定宿主；调用方持有原启动期限。
func (h *Harness) start(ctx context.Context) error {
	if err := h.complete(); err != nil {
		return err
	}
	return h.Recovery.Startup(ctx)
}
