# 核实认证绑定、签名授权与离线时间的机制边界

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: auth_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

Go 单机可自托管的 Harness 如何利用现有标准完成节点及浏览器 UI 的认证绑定、有限期签名授权和凭证加密保存，哪些限制必须由应用层与离线恢复规则承担？

## Scope

核对 Go TLS/X.509、gRPC 与 WebSocket 认证边界，浏览器 WebSocket 的认证及 Origin 限制，JWS/JWT 的签名、受众和持有者绑定要求，Go 单调时间在重启后的限制，以及本地认证加密和密钥保管边界。只核实与待决机制直接相关的官方事实，不选择云平台、不审计全部依赖、不实施安全功能。

## Expected evidence

官方规范或实现文档的链接、核对时间、适用约束、候选取舍及不能作出的保证。事实与项目建议分开。

## Output

研究报告：`docs/research/harness-identity-credentials-and-offline-time.md`。

## Comments

由“确定身份、授权与凭证的执行位置”细化时产生的独立事实问题。授权票仍可推进不依赖这些机制选型的策略与管理操作设计；认证绑定和具体凭证选型待报告核对。

## Answer

已完成官方事实核对，报告：[Harness 身份、凭据与离线时间的机制证据](../../../docs/research/harness-identity-credentials-and-offline-time.md)。研究分支 `research/harness-identity-credentials`，worktree `/tmp/lerna-wayfinder-auth`，初次提交 `636dd61615e4af70bf2be6586ed164465e4b31e2`，ES256 补充提交 `6132555e97f23e382bba075343500df2f5d6cabe`；报告已整合至主工作区。

- Go TLS 与 gRPC 可以提供已验证的对端证书材料，但节点登记、逐操作授权与撤销仍由应用落实；证书链验证不自动检查当前撤销。
- 浏览器 WebSocket 不提供任意认证请求头参数；同源会话 Cookie 与 Origin 校验是可行参考组合，Origin 本身不能认证用户。
- JWS/JWT 签名不加密内容，也不自动实现持有者绑定、委派收缩或一次性消费；证书摘要绑定须与当前 mTLS 对端实际比较。
- Go 单调时间在序列化及进程重启后丢失，部分系统休眠也可能暂停；有限离线执行须处理时间不确定，不能保证未送达撤销即时生效。
- AEAD 可保护本地密文与附加数据完整性，主密钥需独立管理；不防御已经控制解密进程或同时取得主密钥的攻击者，也不单独阻止有效旧记录回滚。

上述为事实和组合方案的依据，不代替用户选择。没有代码、互操作或安全实验。本票未产生需继续独立研究的阻塞问题；具体配置和认证/授权生命周期由授权票确定。

补充核实 ES256 为 P-256 与 SHA-256，JWS 使用固定 64 字节 R || S，Go 的 ASN.1 签名输出不能直接替代。报告建议使用符合 JOSE 的实现处理序列化，未选定第三方库或版本。
