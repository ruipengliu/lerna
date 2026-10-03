# Brain 实现范围

Brain 保留原 Decision、冻结编码、至多一次物理模型请求、原用量及提案发布责任。提案不裁决 Task 成功，也不自行授予行动权限。

私有复合载荷包含 Snapshot、Encoding、Generated 和 Publications，序列化后最多 2 MiB，按原准确字节分为最多 32 个、每个最多 64 KiB 的不可变 `brain.payloads` 记录。Decision 主记录保留原身份、phase、CallID、sendStarted、physical count、费用及载荷摘要/长度/准确分片引用。分片与主记录 CAS、Job 和回执在原同库事务共同提交；单个 Runtime 记录和每个公开 JSON 合同仍保留原 256 KiB 上限。

读取和恢复先核租户、owner、Decision、分片原身份与 revision、索引、长度及整体摘要，再解码固定私有类型。原分片保留到原责任清理规则允许释放，不能在新主头提交时删掉并发旧读者仍引用的分片。缺片或损坏明确失败，不从当前 Content 重建原编码，不重新发送原模型请求；历史 inline 格式仍可恢复。

`payload_test.go` 使用真实 SQLite、HTTP Provider 合同服务、持久文件内容边界和原提交故障点，验证 180 KiB 回复与大编码、数据库重开、原 Lookup、一次 POST、公开 Usage 的实际已知费用、提交前回滚/提交后失回执、原分片损坏及 inline 兼容。该证据不声明真实供应商账户、自然语言质量或未配置的生产部署能力。
