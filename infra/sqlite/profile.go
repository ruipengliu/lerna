package sqlite

// LocalProfileSupported 按平台准入表判定配置；Open 只传入实际观测值。
// 此资格是带存储模型假设的数据库本地档，不授予文件适配器资格。
func LocalProfileSupported(s Settings) bool {
	return s.Platform == "27.0.1/26A434/27.0.0/arm64/apfs" && s.SQLiteVersion == "3.53.4" &&
		s.SQLiteSourceID == "2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc" &&
		s.SQLiteCompileOptionsHash == "a2f6947c17af9ef76e5b18f8ade825805502af9d65a6f1c7a4c547a74ae4c610" &&
		s.JournalMode == "wal" && s.Synchronous == 2 && s.FullFSync == 1
}
