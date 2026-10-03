# 01: Go／TypeScript 准确编码往返

**What to build:** 应用和组件开发者可以用同版 Go／TypeScript 公共类型构造准确引用、修订、金额、时间和集合视图，经公开编解码入口往返后保留原值；新环境可以重建类型并运行共同夹具。

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] 从同一版闭合机器契约生成 Go／TypeScript 类型，覆盖 ID、OwnerRef、ObjectRef、ContentRef、Revision、Amount、Time 和 CollectionView；准确内容引用保留版本、摘要、介质类型和字节长度，不暗含读取权限。
- [ ] 两种语言通过共同正反例验证不透明 ID、非负修订、带明确单位的整数金额和固定精度 UTC 时间；十进制字符串的格式、范围及时间精度明确且一致。超过 JavaScript 安全整数范围的合法值仍能准确往返，金额不经过浮点累计。
- [ ] CollectionView 保留有界 items、cursor、exhausted、partial、gaps、读取范围与水位，不将分页视图解释为全局快照。
- [ ] Go 编码后由 TypeScript 解码并重编码，以及反向路径，均保留准确值；边界正例可用，非法值得到明确验证失败，而非截断、取整或默认补值。
- [ ] 使用根目录单 Go module 和 pnpm 工作区；验证并锁定工具链、生成器及依赖版本，构建说明不依赖当前机器的个人绝对路径。
- [ ] 建立可运行的 bootstrap、generate、lint、test、test-contract、build 和 check 入口，CI 复用同一入口；只包含当前已实现范围，缺失必需依赖或失败不得返回成功。
- [ ] 生成物标注来源、纳入版本控制且不得手工修改；锁定安装及重复生成无差异，共同夹具可以在无外部凭据的环境运行。

## Scope

本任务交付公共值的机器契约、编解码和验证设施；后续任务逐步增加命令、回执和方法。未实现能力不得用占位实现广告为已支持。
