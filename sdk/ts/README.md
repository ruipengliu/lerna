# TypeScript SDK

在仓库根目录运行 `pnpm install --frozen-lockfile`，再运行 `pnpm generate:check`、`pnpm typecheck`、`pnpm test` 和 `pnpm build`。

浏览器通过同源 HttpOnly Cookie 认证；凭据不进入 SDK 账本。`HarnessClient.fromServer()` 获取认证发现、下载原始 core Schema 字节并核对 SHA-256，再编译闭合方法合同。只接受当前发现登记的方法。

```ts
import { HarnessClient } from "@harness/sdk";

const client = await HarnessClient.fromServer();
const owner = client.registry.discovery.logical_service_id;
const page = await client.query(client.makeQuery("task.list", owner, { limit: 20 }));
// page 的闭合响应合同已由原服务发现中的 Schema 验证。
await client.recover(); // 原 owner 查回执；确定 not_found 后才重传原命令。
await client.close();
```

`command()` 在首次连接或发送前提交 IndexedDB 原命令、JCS 摘要、固定 owner、身份 scope、core 摘要和原方法 Schema。存储失败不首次发送。同 ID 的异内容拒绝；重连不刷新 CAS、默认值、截止或逻辑 owner。回执只有在本地事务 complete 后交付调用者。请求等待覆盖响应验证和耐久提交；超时保留原责任。

`preparePublication()` 一次形成 ContentRef、准确原字节、reserve/put 和可选后续命令。`publishOriginal()` 在共同保存后执行原票据上传、发布和后续提交。相同意图可在重开后恢复；accepted 后续命令继续留在 pending。业务拒绝、正文出版、Task 完成和服务端清理分别报告。

当前支持 `harness/1`、`architecture-2026-10-data1` 和 `harness-wss/1`。使用原生 WebSocket，子协议为 `harness-wss.v1`。同 core/profile 的旧方法 Schema 随原命令保存，即使认证发现移除方法仍可解释其原回执。不同 core/profile 需要对应发布版解码器；本 SDK 会明确拒绝，保留原命令，不转换到新版本。

界限：32 项原命令未结、1,000 项命令归档、8 项原正文出版未结、100 项正文归档；普通传输预留 28 个槽，控制/原命令核对保留 4 个槽，总队列上限 4 MiB。正文出版与受信预览上限为 256 KiB。可显式清除当前 scope 的已结浏览器副本；不会删除未结意图或服务端记录。

测试在公开 SDK、CommandStore/PublicationStore、严格 JSON、运行时 Schema 和真实 native WebSocket 边界验证合法正例及拒绝反例。IndexedDB 单元合同使用 fake-indexeddb；真实浏览器持久性与 Go 服务闭环由 [Web 浏览器验证](../../apps/web/README.md) 单独验证。

类型由唯一 [core Schema](../../docs/architecture/protocol/core.schema.json) 生成，运行 `pnpm generate` 更新，不得手改 `.gen.ts`。条件约束由运行时验证执行；TypeScript 类型不替代运行时合同。
