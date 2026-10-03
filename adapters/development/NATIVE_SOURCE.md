# 默认 App 的显式外来消费方 Source

`Config.foreign_consumers` 为最多四个独立消费 owner 开放准确 `content.foreign.register/current/get/release`，合同来自唯一 `providers.ForeignSourceContracts()`。空配置不登记 Source 入口；存在消费者但缺 HTTPS 时 `App.Run(..., serve=true, ...)` 返回 `unsupported:foreign_source_https_not_configured`。构造、恢复配置和读取身份不发送业务 RPC。

## 配对和 HTTPS

先显式迁移 Native 私有数据库，用 `inspect-owner` 只读取得真实 tenant/owner/database_id；将该原 ID 保存为 Native 的 `expected_database_id`。Source 管理配置批准准确 ConsumerScope、独立 peer 的 Source-owner SubjectRef/generation、入站 token 的环境变量引用、holder 的 Consumer-owner SubjectRef/generation/Source-local roles、有限用途和位置。holder 可以为零角色，只得到来源政策中的批准身份；没有可交付的 Source 登录 token。peer 只有 `paired_content_consumer`，不借用 Source 用户完整凭据。

首次管理初始化使用 `OpenApp(ctx,cfg,true)`；正常重开为 `false`。`platform.foreign_consumer_pairs` 冻结真实消费数据库、peer、准确 holder/purpose/location 与入站凭据摘要；替换 DB、generation、roles、token 或 tuple 明确拒绝，旧业务责任不重命名。本版没有自动配对、凭据轮换或原数据库替身。

`foreign_source_tls` 只有绝对 `certificate_file` 和 `key_file` 引用；私钥为私有普通文件，读取大小有界，服务使用 TLS 1.3。Native 的 `foreign_sources` 固定 Source owner/真实源数据库、HTTPS origin/CA、准确 ES256 公钥/kid、peer/current generation、purpose/local位置；Native仍从Source的实际discovery校验四方法摘要。TLS文件与凭据正文不进入记录或日志。

若同时配置 `EndpointChannels`，Source TLS cert/key 必须与其 GatewayTLS 的准确文件引用一致，并复用原 TLS；application 原内部 mTLS 不被替换。不同引用拒绝为 `foreign_source_endpoint_tls_conflict`，没有两个并行 TLS 权威。`RemoteAgent` 并存时只注册一次四方法，按准确已批准 consumer owner 的组合 Authority 路由，原 Agent 主体裁决保持。

## 数据许可和原责任

新宿主默认 ContentPolicy 只合并配置明确批准的 holder/purpose/location，已出版内容的原 PolicyRef 保持。读取继续检查真实源 PolicyValues、当前 Source credentials/roles、当前 Source/accounting/Grant门禁和原 CopyHolder；Native派生政策取来源限制交集。本版配置不是通用 Grant 授予，也不将源 owner 或原 ContentRef 改写为消费 owner。

Native受信 OS 宿主 `prepare-foreign-use` 在 Source RPC 前保存原 reference intent/consumer DB/register/release IDs/Job；失答复恢复沿原 receipt。准确正文分片和完整 hash 保留，普通每次使用取得新 Source.current，control 无 Body，known-deny 永久保留。Source关闭后本方原 stop/release 使用原身份；历史 written 事实保留，实际逻辑删除仍报告 SQLite页/WAL未证明擦除的 residual。

## 实际验证边界

见[App/Native Source测试](../../conformance/alternate/app_source_test.go)和[原配置/关闭/冲突测试](native_source_test.go)。Source为默认 development.App；Native为独立 Node24/原生SQLite进程。Source端分别真实SQLite和PostgreSQL；实际App.Run HTTPS（Go测试宿主）经仅注入网络丢答复的TLS代理与Native互操作。测试记录原登记回执、115000原字节、三用途各两片、Native实际SIGKILL、同原Scope重开、Source关闭、原release和6→6正文片请求。

准确执行版本、日志摘要与旧失败见[Native验证报告](../alternate/VALIDATION.md)。这里不声明默认Orchestrator的整个report已换成Native Brain/Executor；三组件各自有限开放profile及真实独立行为见[Native README](../alternate/README.md)，供应商质量/生产身份/真机/3AZ另行资格。
