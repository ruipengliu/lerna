# 切片 01 架构审查

扫描基线：`b825c2d`。范围为本片最近修改的 codec、严格 JSON、receipt、受信查询、摘要、协商、生成器与合同 harness。领域语言依据根 CONTEXT.md；没有引入第二份 GLOSSARY 或修改领域决定。

## 候选与决定

唯一候选：集中共同合同 harness module 的 runner 生命周期，推荐强度 **Worth exploring**。核心合同 modules 当前已有合理 depth、locality 及真实 seam，不增加新的产品 interface。

158 项夹具中 68 个正例；原逐例模式共启动 226 次 Node 与 226 次 Go。7 次公开 ID runner 顺序采样的中位耗时为 Node 394.62 ms、Go 16.32 ms；空 Node 为 65.15 ms。这是启动及 ID 编解码采样，完整套件总耗时未测；92.87 秒为粗估，不能作为收益结论。

用户授权的决策代理接受当前实施。先修复两轴代码审查问题，再独立分支／提交优化生命周期，保留真实 Go→TS / TS→Go typed 往返、原始字节、逐次 10 秒上限、逐例拒绝分类、有限帧和退出清理。完整约束及取舍见[架构决定](architecture-decisions.md)。

删除测试：删除 typed runners 会使 Schema 选择和公开编解码逻辑泄漏到 harness；应保留它们的 depth。逐例重启的 lifecycle implementation 可以集中，减少重复初始化并让错误处理的 locality 收敛。两种语言是真实 adapter；无需未来多版本／SDK框架。

无 ADR 冲突；保留 ADR-0008 和 ADR-0010 的同版及真实证据约束。

## 报告与测量

视觉报告写入环境临时目录 `/tmp/architecture-review-1791046275195.html`，含 before / after。当前环境无可用桌面浏览器，xdg-open 未能自动打开；报告可从工作区文件链接查看。原采样详情位于 `/tmp/lerna-01-runner-measurements.json`；本记录保留关键事实，临时文件不作为长期证据的唯一来源。

实施前后完整耗时、结果及限制待后置任务完成后追加，不提前声称加速。
