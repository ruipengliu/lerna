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

可通过 `HARNESS_CHROMIUM` 指定 Chromium。脚本不打印凭据；截图和报告写到仓库外。它验证报告准确正文、实际文件写入/独立读回后的 Result、全文预览、真实已应用回执丢失后的 reload/原命令查询、单一 Task 创建、真实暂停/恢复/取消、窄屏导航与注销。仅在 WebSocket 边界丢弃一个真实回执，服务端继续使用真实数据库和业务组件。
