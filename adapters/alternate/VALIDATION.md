# 独立三系统的有界实现证据

2026-10-03，源码 `741dc214fe32839971c961bd735ff30ba87e5e29` 的 19 类场景、含子场景共 26 项，完整 `go test -race ./conformance/alternate -count=1 -json` 实际退出 0。Go 原输出用时 118.546 秒，JSON 包记录为 118.548 秒。三个组件均为独立 Node 进程和独立原生 SQLite 数据库，通过实际 TLS/WSS 与 Go SDK 交接；HTTP 也经真实 Go HTTPTransport 验证。

这份证据覆盖 [README 的有界 profile](README.md)，不表示工单 21 全部完成。外来 Memory 的显式宿主 PrepareForeignUse、默认 Go Source 的原登记与 held gate 仍待实现和验证；默认 Go report 全链替换、供应商与生产运行也未在此验收。

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
