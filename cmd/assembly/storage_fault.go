//go:build fault

package assembly

// StorageFaultSQL 通过同一写连接驱动故障模型的检查点与负对照。
func (h *Harness) StorageFaultSQL(statement string) error { return h.store.StorageFaultSQL(statement) }
