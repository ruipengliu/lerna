# Web 管理界面

在仓库根目录运行 `pnpm install --frozen-lockfile` 和 `pnpm dev`。默认端口 5173；`HARNESS_API_ORIGIN` 指定真实 Gateway，默认 `http://127.0.0.1:8080`。Gateway 必须允许浏览器页面 Origin。生产构建使用 `pnpm build`，可由 Gateway 提供 `apps/web/dist`；此工程不附带部署动作。

工作台通过准确 Content 出版流程提交报告模板，并分别展示 Task 接纳、控制/执行、效果、完成、Result 导出、费用及清理。内容区支持有界准确原文/文件出版。其他管理区仅呈现认证发现中已开放的 Session、分支、Memory、授权、安装及评测方法，缺失能力明确不可用。

受信输入/确认绑定身份 scope、当前凭据修订、原请求版本、目标版本与固定截止。回答 Schema 必须匹配原 ComponentRef 摘要；确认原命令必须匹配原 intent 摘要。必需全文按准确 ContentRef 取回、核对字节数/哈希并成功呈现后，依赖按钮才可用。表单仅支持有界闭合字段和闭合 oneOf；不执行脚本、远程 Schema、HTML 或 Markdown HTML。图像只呈现验真后的 PNG/JPEG 原字节。

Surface 通过 `presentation.open/begin/read/rendered/close` 维护准确呈现。`read` 返回的有限内联原字节必须与全部必需引用逐一匹配；本窗口无缓存时不接受缺正文的 `not_modified`。身份、凭据修订、窗口代数、意图与 Surface 修订共同绑定呈现资格。固定应用事件只来自已认证配置，Schema 和 binding_ref 精确相等时才呈现表单；事件接纳、固定业务消费与呈现分别显示。示例归档入口只作用于配置登记的示例会话。

SDK 的 JSON Schema 验证器解释已验摘要的同版合同，不使用 `eval` 或 `new Function`，可在 Gateway 的 `script-src 'self'` 约束下运行。

关闭界面不取消 Task；退出登录只注销浏览器会话。恢复出版沿原身份查询并推进；已结浏览器副本可显式清除，未结责任仍保留。

## 验证

仓库根目录提供 `pnpm fmt:check`、`pnpm lint`、`pnpm typecheck`、`pnpm test`、`pnpm generate:check` 和 `pnpm build`。

真实浏览器验证需要运行中的完整开发 Gateway、允许的 Origin，以及真实开发凭据文件。不会用假数据服务替代 Go 端到端。Browser 插件未提供，使用锁定的 Playwright 与本机 Chromium。

```sh
HARNESS_BROWSER_URL=http://127.0.0.1:5173 \
HARNESS_TOKEN_FILE=/workspace/lerna-dev/.identity-token \
HARNESS_BROWSER_ARTIFACTS=/tmp/harness-web-browser \
pnpm test:browser
```

可通过 `HARNESS_CHROMIUM` 指定 Chromium。脚本不打印凭据；截图和报告写到仓库外。它验证报告准确正文、实际文件写入/独立读回后的 Result、全文预览、真实已应用回执丢失后的 reload/原命令查询、单一 Task 创建、真实暂停/恢复/取消、准确输入消费、Surface 呈现、固定应用事件、窄屏导航与注销。边界故障只丢弃一个真实回执或破坏一次准确正文响应；服务端继续使用真实数据库和业务组件。

由 Gateway 提供生产构建时可设置 `HARNESS_REQUIRE_CSP=1`，要求页面使用禁止内联脚本及动态求值的同源 CSP。`HARNESS_REQUIRE_LARGE_MANIFEST=1` 要求实际方法清单超过 256 KiB 且不超过 1 MiB。报告记录真实方法数量、字节数、摘要和连接身份绑定。

验证报告保存脚本实现 commit、实际身份 scope/修订、服务与租户、策略准确引用、Schema 摘要以及 Node/Chromium 版本。`HARNESS_BACKEND_COMMIT` 可记录操作者已核实的后端构建 commit；该字段明确标记为声明信息，不能由方法摘要反推实现版本。

`HARNESS_BROWSER_FLOW=surface` 或 `control` 可单独验证相应链路；完整流程的失败记录仍保留。控制遇到真实版本冲突时，脚本重新读取原 Task，显式点击产生不同 ID 的新意图，最多四次；SDK 不修改已保存命令的 CAS。`HARNESS_CONTROL_TASK` 可在 control 分段中恢复并最终取消原未结测试 Task，不另建目标。固定示例会话首次归档要求 applied；后续重复运行必须显式设置 `HARNESS_EXPECT_EVENT=rejected`，核验旧 CAS 被拒以及原会话仍已归档，不能重置历史。

`HARNESS_ORIGINAL_TASK` 指定实际已发布 Task 后，`node apps/web/tests/read-original.mjs` 只读原 Task、原 Result 与准确全文，不提交新业务命令。探针跨两个真实服务端心跳后继续查询，并核每次刷新使用新 query_id。`HARNESS_EXPECTED_ARTIFACT_FILE` 可指定原报告文件核对全文；`HARNESS_PROXY_OBSERVE=1` 对比透明 WebSocket 转发边界。其余地址、凭据及产物环境变量与完整脚本一致。

同一已发布 Task 也可用 `node apps/web/tests/session-guard.mjs` 验证原 Cookie 会话注销后的连接守卫。脚本保留原生 WebSocket，直接注销自身浏览器会话，再发只读查询；服务端必须关闭旧连接且不返回该查询的领域响应。随后通过新 Cookie 重新认证，核对原 Task、Result 与准确 ContentRef 相同，全过程不提交业务命令。修复前后的运行应分别保存产物目录，并为每轮填写已核实的 `HARNESS_BACKEND_COMMIT`；失败轨迹不可覆盖。
