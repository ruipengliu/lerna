# 独立 TypeScript 系统组件

这里提供第二套 Brain、Memory、Executor 系统实现。三个进程各自保存所属 owner 的业务记录、原命令、回执和持久工作，使用 Node 原生 SQLite；它们不实例化 Go Service、不共享 Go 业务数据库，也不把更换 Engine 或 Store 当作系统替换。公开入口只有同版 JSON 合同和真实 TLS/WSS。

依据为[扩展合同](../../docs/architecture/extensions/README.md)、[系统端口最低族](../../docs/architecture/protocol/README.md)和[共同方法合同](../../docs/architecture/protocol/method-contract.md)。准确开放方法由认证后的 `/api/discovery` 返回。下列范围是有界参考 profile，尚未覆盖完整可选族或生产部署。

## 开放范围

| 组件 | 开放方法 | 实际行为与边界 |
| --- | --- | --- |
| Brain | `brain.decide/get/cancel` | 独立 Decision、call、提案出版和工作账本；只运行准确登记的 `alternate-answer1` 模板。`answer` 生成准确正文；自由文本提出 `clarify_goal` 输入请求。其他模型、report 工具编排和任意供应商请求均未开放，物理模型请求数与 USD 费用均为 0。Brain 提案不裁决 Task 成功 |
| Executor | `execution.invoke/get/list/cancel/control/control.get/reconcile/usage.get` | 独立 Operation、原 Attempt、签名 Task gate 和用量依据；只开放准确登记的 Go `file.read` 合同和 InstallLock。Linux 私有根目录里读取单个普通文件，至多 64 KiB、至多一次 Attempt；不接受路径越界、符号链接、硬链接、FIFO、资源扩张或非零费用 |
| Content / Memory | `content.policy.install/upload_reserve/put/close/get/transfer.read`；`memory.create/replace/delete/read/inspect/list/query/index.inspect/cleanup.get/view.open/view.pull/view.ack` | 独立准确正文、来源门、Memory 版本、变更头、检索快照和 View。同步元数据权威扫描与有限字面检索，不提供向量、Extraction、Experience、任意远端索引或可重命名 owner 的缓存 |

有界方法之外返回 `unsupported`。Memory 的更正、删除、inspect/read/list/query、cleanup 和 View 属于同一开放族；Executor 的 invoke 与 list 同时提供。`memory.cleanup.get` 返回当前获准管理的 `MemoryRecord` 元数据，不读取已撤回正文，也不触发清理。

## 合同、依赖和运行

`contracts/` 只生成线 Schema 和准确组件引用，生成器可在仓库内读取 Go 类型；导出签名不要求外部组件引用 `internal` 类型。`ts/src/contracts.gen.json` 从同版核心 Schema 和方法类型生成，不能手改。核心原字节摘要、所有方法 Schema 摘要和受支持 profile 都参与验证；每个数据库冻结原 owner、配置与合同摘要，启动不会把旧责任换成新合同。

工具链沿用根锁：Node **24.19.0**、pnpm **11.19.0**、TypeScript **7.0.2**。实际原生 SQLite **3.53.3**。新增 workspace 复用已有 SDK、`ws 8.19.0` 和 `vite 8.3.2`，没有新增第三方依赖版本。Vite 只打包 Node 入口，`node:*` 与 `ws` 保持外部模块。默认网络库、SDK 和目标读取均无隐式重试。

```sh
pnpm install --frozen-lockfile
pnpm generate:check
pnpm --filter @harness/alternate build
node adapters/alternate/ts/dist/main.mjs migrate --config /absolute/private/owner-config.json
node adapters/alternate/ts/dist/main.mjs serve --config /absolute/private/owner-config.json
```

迁移必须显式执行。`serve` 只监听 loopback HTTPS，原生 WSS 子协议为 `harness-wss.v1`；HTTP `/api/call` 和有限正文通道同样认证。配置和管理记录必须是当前 OS 用户的私有普通文件，数据库目录和 Executor 根目录必须为该用户私有目录。凭据提供准确 token hash、subject、generation、roles、expiry、active；跨组件 bearer 通过私有 `token_file` 引用，CA 通过固定文件引用，不写入日志。

配置的闭合 Schema 见 [config.ts](ts/src/config.ts)。Brain 配置引用生成资产里的准确 `brain_profile` 和 `answer_schema`；Executor 引用准确 `read_capability`、固定 Binding/InstallLock、私有 managed root；两者都配置独立 Content owner 的固定 HTTPS origin、CA、policy 和位置。每个 owner 有独立 SQLite 文件，三个系统不可共用同一文件或篡改原 `owner_id`。

受信 OS 管理入口仅维护当前 credentials、不可变 UseReceipt 和永久 known-deny：

```sh
node adapters/alternate/ts/dist/main.mjs admin --config /absolute/private/owner-config.json \
  --namespace denials --record /absolute/private/original-denial.json
```

`uses` 必须是注册 ES256 authority 真实签名的原 UseReceipt，绑定 tenant、subject/generation、target、recipient、intent hash、purpose、location 和原有限时间窗。撤权检查原 use、父 Grant 和 subject 的 known-deny；当前凭据每次传输、裁决和披露都复核。修改 credential 的 token、roles 或 expiry 必须升级 generation；旧 generation 不会因重新运行 migrate 或 serve 而复活。该 profile 使用有限窗口与本地已知撤权，不宣称即时获知尚未送达的云端撤权。

## 原责任与故障恢复

- SDK 首发前持久化原命令；服务端同库短事务同时提交业务事实、回执和新增工作。外部 Content RPC 和真实文件读取均在事务外执行。提交未知只查原身份；不以未知提交开始外部工作。
- 新实例先提交独立 instance epoch，旧实例随后不能再提交。裁决时间取本机时钟与已保存裁决时间的最大值。构造对象、恢复反序列化和管理入口均不发送业务请求。
- 原 Content 出站命令、准确 bytes、owner、输入摘要、方法/core Schema 和 deadline 先入本方 outbox；失答复先查原回执，只有原 `not_found` 才重传准确原命令。原 deadline、retention 和默认值不刷新。出版恢复至多 8 次交接尝试，每次都沿同一原命令；它不会增加模型或文件 Attempt。
- Executor 在实际读取前持久化 Attempt/start barrier。若进程在 barrier 后消失且没有独立 observation，保留 `effect=unknown`，不重新读取后来改变的目标，不换身份，不接受客户端宣称效果。PID、启动时刻和 Linux boot ID 只用于观察原进程是否已退出；`actually_stopped` 与未知效果分别报告。
- Memory 保留原准确 ContentRef/hash/version/来源闭包。来源关闭或到期立即禁止读取，持久到期工作恢复后关闭本地 holder。Memory 的本地 holder 清理与 Content 全部物理副本擦除不同；SQLite 页、WAL 和外部副本未证明擦除时，Content 始终报告 `cleanup_state=residual`。
- 原 query 已封存也复核当前权限。版本或结果变化要求新 query ID；旧封存不能重放已撤回正文。cursor 使用 HMAC，绑定原输入、subject/generation、快照和当前可见性；View 只承认原 issued cursor 的 ack，不把 ack 解释为人工阅读。
- 原生 cookie session logout 关闭该 session 的已有 WSS，原发布责任保持。SIGINT/SIGTERM 关闭连接、停止工作并等待实际退出后关闭数据库。

每个 owner 最多 900 普通记录 / 9,000 历史记录，额外保留 100 / 1,000 给控制和当前撤权。Content 单个 256 KiB、全部活跃 BLOB 32 MiB；Memory/Operation 200 个、来源图 64 个节点、32 个待处理工作；普通页最多 20 个、cursor/View 5 分钟、Task control start window 5 秒。WSS 每连接 32 个 pending，其中 4 个控制预留，发送缓冲普通 3 MiB / 控制总额 4 MiB。达到上限会拒绝新责任，旧去重和费用身份不删除以制造容量。

## 实际交接与仍关闭的能力

Go SDK 通过真实 TLS/WSS 调用三个 Node owner；Brain、Executor 通过固定 HTTPS 与另一个独立 Content owner 交接准确 Snapshot、意图、控制证明、正文和出版回执，输出引用继续属于那个 Content owner。Brain 的模板编码为准确原 goal bytes 的 base64 加 Snapshot 的 JCS 编码；使用该模型 profile 的宿主必须按该准确编码构造 Snapshot，不能把默认 Go 模型的编码重命名后交给本组件。

当前 Native Memory 写族只接纳自己所属 Content owner 的原引用；外来 ContentRef 返回 `dependency_unavailable/foreign_content_registration_required`。向默认 Go Memory 注册外来副本需要工单 23 的原 owner reference intent、当前 held gate、用途/位置/期限与 known-deny 合同；没有这份实际登记前不能绕过 owner gate。这里的跨语言系统端口验证不宣称默认 Go Orchestrator 的整个 report pipeline 已切换为本组件。

真实供应商模型、远程写工具、任意重试、完整设备/多租户部署、异地复制、规模和灾备仍未开放。独立本地 TLS 进程证明异构系统交接；它不代表公网生产部署已验证。

## 验证

```sh
go test ./conformance/alternate -count=1 -v
go test -race ./conformance/alternate -count=1 -v
pnpm fmt:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm generate:check
```

`conformance/alternate` 使用真实独立子进程、临时私有数据库、实际 ES256/JWS、原生 Go WSS 与真实磁盘文件。它验证准确正常正文、Memory 更正/撤回/cleanup/View、原回执丢失和 SIGKILL 恢复、当前 Use purpose/撤权、原签名暂停、伪签名拒绝、target start barrier 后不再次读取、坏 cursor/未知方法、credential 撤销持久性、cookie logout 和原到期工作。每轮日志记录实际 owner/scope、原引用、core/method digest、Node/SQLite 版本；夹具 authority 只签原合同，没有替代任何业务 Service 或目标真值。
