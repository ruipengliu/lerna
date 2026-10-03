# 独立三系统的有界实现证据

2026-10-03，源码 `741dc214fe32839971c961bd735ff30ba87e5e29` 的 19 类场景、含子场景共 26 项，完整 `go test -race ./conformance/alternate -count=1 -json` 实际退出 0。Go 原输出用时 118.546 秒，JSON 包记录为 118.548 秒。三个组件均为独立 Node 进程和独立原生 SQLite 数据库，通过实际 TLS/WSS 与 Go SDK 交接；HTTP 也经真实 Go HTTPTransport 验证。

这份最初的 19 类证据覆盖 [README 的有界 profile](README.md) 基础范围；后续外来 Memory 的显式宿主、准确 Source 登记与 held gate 验收见下文。默认 Go App 的 Source 配对／Grant 装配、默认 Go report 全链替换、供应商与生产运行均不能由本页的独立进程夹具推断。

## 固定版本与制品

| 资产 | 准确绑定 |
| --- | --- |
| 源码 | `741dc214fe32839971c961bd735ff30ba87e5e29`，整轮开始时工作树干净 |
| Node 构建 | `sha256:49d45f6ffa5792986894e42f35fc4e5370c5ccc29617fc4ad6d7702e0d18880a` |
| 同源生成合同 | `sha256:507898515d8260753a553215abc100f7f432191c72aebc5348566db02eda93da` |
| core 原字节 | `sha256:168c24b7f7a29b4e64b5e35fedb76333b9b259920dd51f2de4f4fdb4d8742eaf` |
| Brain 3 方法 | `sha256:9a252c340b775e488aec0fec44fefe9b403eef4ed14cdaf988b80287935ca293` |
| Executor 8 方法 | `sha256:d29868ecf239ee242dfa9211497ddbf91a226641f3ab41f70248e212102694cf` |
| Content/Memory 20 方法 | `sha256:3f73b3f6e365d02a2d5e4617ca0d63cbd1ce1cb02dc6bf0a6c6cfddb7bfd25c0` |
| 整轮 JSON 日志 | `sha256:788817a37f16fb8a8d444210e9a134eaa3f2bd823437e2e58804264ee49e7da3` |

本地完整制品为 `/workspace/harness-alternate-qa/base-closed-741dc21/{race.jsonl,metadata.json}`。metadata 保存每类场景、用时及各原 owner/tenant/subject、identity_scope/generation 与方法摘要。测试源为 [system_test.go](../../conformance/alternate/system_test.go) 和 [http_test.go](../../conformance/alternate/http_test.go)。工具链为 Go 1.26.8、Node 24.19.0、原生 SQLite 3.53.3、pnpm 11.19.0、TypeScript 7.0.2。

夹具显式迁移私有临时数据库，配置原 credentials、固定 TLS CA/origin、注册 ES256 authority、有限当前 Policy/Use 和 Linux 私有文件根/Binding/InstallLock。ID、签名 key 与临时凭据来自 OS 随机源，没有可伪称的固定随机种子；完整日志保留准确非秘密身份，临时私钥和凭据不保存或打印。夹具许可不替代任何 Memory、Brain、Executor Service、回执账本或真实目标文件。

## 实际行为

| 范围 | 已观察的正反例和故障 |
| --- | --- |
| Memory/Content | 原策略、准确字节、更正、撤回、有限 query/list/View、坏 cursor、cleanup；原 CopyHolder/reference intent、控制事实和残留报告；原到期工作在实际停机后恢复；credential 撤权重开仍有效 |
| Brain | 准确 Snapshot/原 goal bytes、独立 Decision、answer/clarify 提案与准确出版引用；原失答复/SIGKILL 恢复、当前来源关闭、原 Use/Grant known-deny 和取消；物理模型次数及 USD 都是 0 |
| Executor | 真正文件读取及独立 hash 真值、原 Operation/Attempt/usage/list；真实 ES256 pause、伪签名/错 purpose 拒绝；符号链接、硬链接、越界、FIFO 都无 Attempt；start barrier 后实际 SIGKILL 保留 unknown，后来目标变化不会再读取 |
| 传输/原责任 | Go 原生 TLS/WSS、严格发现和原 schema；真实 HTTP `{result_kind,payload}`、原命令/receipt 与业务 error；原 cookie logout 关闭旧 socket；失答复恢复沿原 ID/digest/deadline，不创建新物理行动 |

原回执、接纳、实际效果、费用、清理和当前门禁分别检查。Source 引用的 owner/hash/version 没有改写。最后生成器对齐根 Go 的 `context_lookup.kind` 闭合枚举；这不开放 Native Brain 的 lookup 能力。

## 保留的失败与检查

`base-final/race.jsonl` 保留源码 `6adb16a` 的整轮失败，实际退出 1、172.148 秒。Pause 夹具两次取时使签名窗口跨毫秒超过原 5 秒；改为单一 issuance instant 后 `412237e` 的原 18 类场景退出 0、120.033 秒。业务控制窗口始终是 5 秒。

`http-envelope/protocol-red.jsonl` 保留真正 Go HTTP 查询的 `invalid_payload` 失败，2.929 秒；共同外壳修正后同源 HTTP/原回执及真实文件结果专项退出 0、5.750 秒。`7c4523a` 的 19 类整轮退出 0、134.567 秒，但随后生成漂移检查发现旧 proposal 枚举；生成后再以本页准确源码完整复跑。

本切片实际完成 fmt/lint、三个 workspace 的严格 typecheck、8 个文件 26 项 TS 测试、SDK/Web/Native 构建、生成漂移与受影响 Go vet/diff 检查。新 Native 目录已纳入根共同检查入口。当前文档扫描器不覆盖 adapter 下所有嵌套文档，本页和 README 的相对链接另行实际核对。此处不声称托管 CI、最终整库浏览器或生产验证已执行。

## 外来 Memory 与原责任恢复的完整增量验收

源码 `4840ccc856f033509c641e8433f1e093047fcc64` 开始和结束时工作树干净。
`go test -p 1 -race -count=1 -timeout=30m -json ./conformance/alternate` 实际进程退出 0，
JSON 包记录用时 **343.142 秒**；既有 19 类与新增 9 类全部通过，含子场景共 **40 项**。
30m 只是测试包 runner 上限，原命令、5 秒 control、原 source retention 和原 10 秒 Claim 没有延长。

| 资产 | 准确绑定 |
| --- | --- |
| Native 源码 | `4840ccc856f033509c641e8433f1e093047fcc64`；同源 Go Source 依赖来自根 `97dc3e6` 的祖先 |
| Node 构建 | `sha256:1d601335ff16061f7589ea547ec535bdce2e520ee44c0f94eac8466af38fc995` |
| 生成合同 | `sha256:39a7bfe8815c1016cca613f6f6df92afbb0dfeb646439cb17d124876fa9da1fb` |
| 整轮 JSON 日志 | `sha256:e5ab4238ba08f6ead543904d73a0673d2314737e5b3db7f0c61edf0a0ac4d254` |

完整制品为 `/workspace/harness-alternate-qa/foreign-closed-4840ccc/{race.jsonl,metadata.before.json,metadata.json}`；
metadata 记录实际退出、每类结果、源码／bundle／contract／日志摘要、8 组真实 Source scope、
70 条 Native owner/scope 证据及原数据库事实，不保存 token 或私钥。
新增测试源为 [foreign_test.go](../../conformance/alternate/foreign_test.go)。
核心及三个系统开放方法摘要保持上表的准确值；四个实际 Source 方法来自唯一
`providers.ForeignSourceContracts()`，不是手写 Schema 副本。

| 新行为 | 实际观察 |
| --- | --- |
| 原 source 与准确字节 | 独立 Go SQLite／objectstore／HTTPS Source 与独立 Node SQLite／TLS WSS；152 KiB 正文实际分片读取、全量 hash/length 校验，原 owner/version/hash 不改写 |
| 原登记失答复 | Source 已提交原 register 回执后真实断开 TCP；本方保留 reference intent／Job，恢复先查同一 command ID，原 digest／输入／期限／回执完全一致 |
| 写后失当前答复 | 实际 BLOB 已写后 Source.current 返回 503；held gate 不开放，原 writing／write_intent／written 和 cleanup=pending 仍在 |
| 真正进程丢失 | Host 在已写 BLOB 后实际 SIGKILL；原当前 control、原 release 与 SQLite residual 恢复，已关闭源不再次读取 |
| 原 Job 自动恢复 | 新 Node 等原 Claim 期限后接管同一 Job；检查路径没有另调用 prepare/stop，原 register 回执／release ID／consumer DB 保持，源正文 GET 计数 1→1 |
| 当前权限与期限 | 实际 current authority 撤权、签名 closing proof 与永久 known-deny；恢复 permit 不复活原 copy。原 retain_until 到期后新 proof 不续期限，cleanup 仍 pending，control 不读取 Body |
| 绑定拒绝 | 错 source DB、错登记 key、错 holder generation、改 source owner、真实签名 control 冒充 use 均在源 Body 请求前拒绝 |
| Memory 与投影 | 外来 query_spec／准确 text_ref 新 proof、list 的原 purpose、有限 query、view open/pull/ack、实际 CAS 更正 r2、Source 503 时明确 partial/gap、关闭后 quarantine r3、旧 View 要求新快照、当前 watermark |
| 本方数据库 | 显式 migrate 保存永久随机 ID，inspect-owner 只读；实际 SIGKILL 后原 ID 和正文保持。expected_database_id 拒绝另一个 ID，原文件缺失时 migrate 不用新空库代替 |

夹具声明固定 peer／holder 配对，并从实际 `platform.credentials` 检查当前身份和许可；
CopyHolder、Policy、Source.current 整体 ES256 签名、回执和准确 Body 都由实际 Go Memory／Source 生成。
Native 对每次业务使用在线查 current proof，Source I/O 在事务外；constructor 没有出站，
OS 宿主只在显式 prepare 或恢复原 Job 时交接。默认云端 App 的 `ForeignSources`／真实 Grant
装配仍由所属集成切片继续，本页不把此夹具当成默认 App 配置已经开放的证据。
冻结的 peer generation／schema 不兼容时明确 fail closed，未实现自动凭据或旧 schema fallback。

保留的新增失败在 `foreign-host/`：`public-cli-red.jsonl` 为未知 foreign_sources 的实际
启动拒绝，2.927 秒；`foreign-query-spec-red.jsonl` 为准确外来 query_spec 的 text_ref
未取得当前 proof 的真实 `dependency_unavailable`，29.830 秒，获准准确解析后在 Tx 外
展开原 text_ref 的修复专项为 35.070 秒。`fault-first.jsonl` 实际退出 1、37.183 秒：
前两项故障已通过，最后一项误将显式 prepare 同步完成的 released 期待为 stopping；
改为正常业务 content.get 的撤权门禁路径后专项通过 15.020 秒。完整源码按上述 SHA 再全量复跑，
这些失败没有删除或写成通过。

本轮重复完成根 fmt/lint、三个 workspace typecheck、8 文件 26 项 TS 测试、三个构建、
同源生成漂移、受影响 Go vet／diff 和嵌套文档相对链接实际核对。最终全仓检查、最终冻结
backend 的浏览器和生产验证仍各自取证，不由这轮 40 项推断。
