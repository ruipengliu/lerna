# 准确外来内容的源端传输

`NewForeignSource(ForeignSourceConfig)` 和 `ForeignSourceClient` 连接实际 Memory 的原副本合同。
构造不出站、不登记副本、不安装许可。宿主明确提供 Store/Scope、Memory、固定 ES256
签名 key、公钥信任和纯 Tx Authority，并在同版 Registry 中登记原 Memory 方法后调用
`ForeignSource.Register`。固定传输、当前 peer 身份和持久 SDK journal 由宿主提供。

| 方法 | 闭合输入 → 输出 |
| --- | --- |
| `content.foreign.register` | 原 `memory.ForeignReference` → `memory.CopyOutput` |
| `content.foreign.current` | `{reference, control}` → 完整 `memory.ForeignProof` |
| `content.foreign.get` | `{reference, chunk_index}` → `{content_ref, chunk_index, chunk_count, data_base64}` |
| `content.foreign.release` | `{reference, report: memory.ReleaseCopyInput}` → `memory.CopyOutput` |

register/release 直接调用同库已登记的原 content.register_copy/release_copy 处理器；
原 holder、来源、cleanup、Job 和回执由真正 Memory owner 保存。别名只增加显式配对及
准确原 reference 绑定，不能替代原 DataPolicy。登记命令使用原 RegisterCommandID 和
RetainUntil，释放使用原 ReleaseCommandID；释放首次固定十分钟命令期限，与数据保留期分开。
重传、丢回执和重开沿原 journal 保存的身份、payload、decoder 和期限。

`ForeignSourceAuthority.ResolveSourceSubjectTx` 必须核当前已配对的 peer、consumer、
holder/主体范围，以及宿主要求的原 Grant/委派门禁。它只能从当前受信记录解析原主体，
不能从请求角色造 Auth，也不能出站。源 Memory 继续检查实际 PolicyValues、当前凭据、
用途、处理/接收地点、保留期、所有实际来源及原 CopyHolder。

current 签完整 ForeignProof 去掉 Proof 字段的 JCS 摘要。claims 的 purpose 为
`foreign_content`，issuer 为原 Content owner，audience 为原 Holder owner，ObjectRef
为原 Content 的 owner/id/version，WindowID 为原 CopyID，control_revision 取实际源版本。
SourceDatabaseID 固定真实源数据库，证明窗口三十秒。control 只提供原责任的控制和清理；
client Read 和消费方 held gate 均拒绝用 control 证明授权正文。Executor 的设备来源使用
其独立 `executor_content` 合同，不能混用签名 purpose。

get 每片最多 64 KiB 原字节，最多 256 片。源端每次读前后都核原副本当前门禁；
client 核准确片位置/长度、完整 hash 和 ContentRef，不改变 owner 或以 latest 替代版本。
普通新使用需要当前在线证明；传输失联不把已缓存字节变成许可。内存门禁、有限跨库窗口、
本地字节删除和源 cleanup 的区别见 [Memory 实现](../../internal/memory/README.md)。

`conformance/providers/foreign_source_test.go` 使用实际 HTTPS、两个独立 SQLite owner、
真实 Memory/ObjectStore 和原 SDK journal，验证物理断连后的同登记恢复、160000-byte
准确分片、未绑定主体零出站、当前源关闭、control 禁止正文、原 Stop/Release 与原 receipt。
显式配对授权是有限夹具；该证据不证明生产身份平台、独立进程 Agent 委派或完整 Task 接线。
