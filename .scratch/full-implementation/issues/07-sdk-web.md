# 07 sdk-web

Status: ready-for-agent
Blocked by: 01

依据 [实施规格](../spec.md) 与根 AGENTS.md，保留准确身份、负责方、事务、门禁和恢复。实际编译、公开接口行为、正反例及所需平台证据均通过后才关闭。

## Comments

### 2026-10-03 — TypeScript SDK、Web 实现与真实浏览器证据

已实现严格 JSON/JCS、同源原始 Core Schema 字节摘要核验、发现清单及方法输入/输出运行时校验。领域数据仍限制 256 KiB，方法清单单独限制 1 MiB。浏览器使用原生 WebSocket 与按身份 scope 隔离的 IndexedDB；原命令在 strict 事务 complete 后才首次发送，接纳回执保留未结责任，恢复先查询原负责方，只在权威 not-found 后重传原 ID、摘要、CAS、默认值及截止。旧连接响应按代数隔离，普通队列和控制预留分别有界。

React/TypeScript/Vite 工作台提供准确内容出版、报告提交、输入/分支、Memory、控制及认证发现内的管理方法。受信输入和 Surface 只在准确全文验真、当前身份/凭据修订/请求版本/呈现代数重新核验后开放依赖动作；闭合有界字段与 oneOf 不执行脚本或远程 Schema。接纳、执行、效果、任务完成、Result 出版、费用和清理分别呈现，不能由一项事实推断另一项结束。缺失能力仍显示不可用。

前端检查：`pnpm fmt:check`、`lint`、`generate:check`、`typecheck`、`test`、`build` 全部通过；最新检查包含 8 个测试文件、26 项正反例，包括普通 28 项查询占满后 `schedule.resume` 仍进入控制容量且不改变原命令。Node 24.19.0、pnpm 11.19.0、TypeScript 7.0.2；真实浏览器为锁定 Playwright 1.63.0 与 Chromium 151.0.7922.173。Browser 插件未提供，使用该本地 Playwright 后备。

#### 全流程与原责任只读验证：实际二进制 877730f

运行中的 Go 二进制为 `877730faf274d48cada1821fe973101f58d92479`、`vcs.modified=false`，SHA256 为 `62c9da383a087bbd876f6e3b4adfff0ba870746da1060d69c0166dbfeb01bfe9`。使用真实 PostgreSQL、同源 Gateway 生产构建 `index-cmW-EJ45.js` 和严格 CSP，未使用假数据服务。认证清单为 176 个方法、291898 字节，Core Schema 摘要为 `sha256:168c24b7f7a29b4e64b5e35fedb76333b9b259920dd51f2de4f4fdb4d8742eaf`。报告声明的 backend commit 与操作者实际核实的二进制版本分别记录，不从清单摘要反推源码版本。

[`browser.mjs`](../../../apps/web/tests/browser.mjs) 在原 90 秒 Result 上限下 full 流程通过，证据为 [fresh-full-877730f/report.json](/workspace/harness-web-qa/fresh-full-877730f/report.json)，同目录含桌面报告、Surface 与 390px 窄屏截图。实测包括：准确上传/出版，真实文件写入及独立读回，准确 Markdown 全文，丢弃一个真实已应用回执后的 reload/原命令查询和单一 Task 身份，破坏一次必需正文时输入禁用、修正准确正文后原请求消费，原 Goal 与 amendment 顺序保留，真实暂停/恢复/取消，Surface 坏正文及无本地缓存的 `not_modified` 门禁，固定应用事件消费，关闭后旧代数正文失效，授权 Memory 集合读取、全部管理导航、窄屏无横向溢出及注销。三项 Result 初次观察均为 `published`，此时 `accounting_open=true` 如实保留，没有把出版当费用结清。

| 验证目标 | 原 Task | Result r1 |
| --- | --- | --- |
| 报告与准确全文 | `task_b487b8c4cb00f74142185581b8b774b0` | `result_6755f7957fe233ff759e58f76731a4eb` |
| 丢回执后的原命令恢复 | `task_0e12a6b39d9222085821089629974b2a` | `result_128bbf12d395581050610d625fc3e50b` |
| 准确输入与澄清后的报告 | `task_a10ae823c5964536a5d870b2c75d5785` | `result_6e093a1a8e0823de288fd5dfbf383d78` |

首个 Surface 应用事件 `application_event_37ccd5085fcd5393aa2328b797ce14ba` 从 queued 推进到 applied，准确原命令 `command_7b576302c69617d50af1026b06d323f1` 使用 `session.archive` CAS1；真实示例会话 `session_42ce1d8551ef8db7a9e3fa101a344bc9` 变为 r2、archived。事件接纳回执和实际业务消费分别取证。该身份范围为 `tenant_8737ce6897eaef25abd6241f4fc61418` / `owner_70449eafd4ba9cae2032ae0b11430289`，没有复用或重置旧失败范围。

[`read-original.mjs`](../../../apps/web/tests/read-original.mjs) 在同一 877 二进制、原已完成 Task 和准确 Result 上，通过两次 35 秒原生 WebSocket 心跳只读验证；6 次查询各用新 query_id、0 业务命令、0 页面错误。原 Task 此时 r34、succeeded、`accounting_open=false`；原 Result 与 4725 字节 publication ContentRef（`upload_6067a7967db5c6d3d9dd6cd4face5314`、版本 1、`sha256:866b4b499c806508b7fd58feffcd1db348dfa1666c656040bb18ae5c22c5cdb9`）保持相同，准确正文与原实际文件一致。证据为 [fresh-native-original-877730f/original-result.json](/workspace/harness-web-qa/fresh-native-original-877730f/original-result.json)。

#### 原 Cookie 注销守卫：RED 877 → GREEN 60ad341

新增 [`session-guard.mjs`](../../../apps/web/tests/session-guard.mjs)，只复用上述已完成 Task、原 Result 及准确 ContentRef，0 业务命令。探针直接注销自身 HTTP 会话，保留原生 WebSocket，发送新只读查询，再用新 Cookie 重新认证；不通过客户端主动关闭替代服务端检查。

- RED：实际 877 二进制注销返回 200 后，旧连接的 `task.list` 查询 `query_1ec1362021acc1d9e7db31ced105d23e` 仍收到 `query_result`，15 秒内未关闭。该失败及完整帧轨迹保留在 [session-guard-red-877730f/failure-trace.json](/workspace/harness-web-qa/session-guard-red-877730f/failure-trace.json)，未覆盖为通过。
- GREEN：旧 877 实际退出 0 后，在同配置、数据库、租户及主体上启动实际 `60ad3417d39919c2a64d12149ee4ea362d4cd415`、`vcs.modified=false`，SHA256 `00ac915ee8d4fec764ff12e9992cd433cb20ed33365613b98102f5a09a358a03`；生产 JS 为 `index-BQd2pCXK.js`。注销后旧连接被服务端关闭，查询 `query_21ae1041a86436d1ed2ad0c5fd2c726b` 未收到领域响应；新 Cookie 的第二条连接读取同一 Task、Result 和完整 ContentRef，succeeded、费用已结，0 业务命令、0 页面错误。证据为 [session-guard-green-60ad341/report.json](/workspace/harness-web-qa/session-guard-green-60ad341/report.json) 与 [二进制及原范围绑定记录](/workspace/harness-dev-environment/session-guard-backend-60ad341.json)。

60ad341 的通过仅覆盖本守卫专项，不能替代 877 的全流程证据，也不代表后来提交已完成整体验证。旧 731 澄清目标 source 闭包失败与早期 90 秒超时证据继续保留；原失败终态、Task/Result、示例会话及业务责任未重置。后续同范围全流程必须设置 `HARNESS_EXPECT_EVENT=rejected`，核验旧 CAS 被拒和原会话仍为 archived，不能再把重复归档写成首个 applied。

新增 Search/Body、GUI、WASI 路径及最终锁序集成仍 pending；最后实际二进制的受影响全流程回归另行记录。该记录不改变总规格的 partial 判定、C3/V2 缺口或生产平台资格要求。
