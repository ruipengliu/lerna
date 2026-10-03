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

## 实施退出与实际测量

后置任务 [07](issues/07-runner-lifecycle.md) 已完成。正确性修复后的固定基线为 bb9241f；本独立优化保留 typed runners 的 module depth 与真实 Go / TS adapters，将私有 process lifecycle interface 收敛到 start / run / close。schema 和业务验证没有泄漏到父进程，原单次 CLI 与首次初始化的新进程测试保留。

相同 158 项 / 68 个正例的完整前向 test-contract（含 build、digest / query / negotiation suites），基线实测 119.217268 s、Go / TS fixture runner 各启动 226 次；最终实测 10.909816 s、各启动 1 次。额外反序完整 suite 为 6.406244 s；新增 16 项 lifecycle fault / 正常控制测试为 6.378595 s。原 92.87 s 采样外推没有用于 before / after 或收益结论。最终 make check 和 Go race 全部通过；详细环境、命令、实际计数方法、red→green 和测试成本见票据。

有限 cleanup、私有帧 / stderr 上限、逐请求 timer 与严格响应绑定增加了具体测试设施成本；没有通用 worker pool 或产品 RPC 抽象。保留这个独立优化，因为本地实测显著减少重复初始化，新增设施同时提供先前未有的有限故障证据。结果限于本机这次配对测量，不代表未来生产吞吐或固定 CI 耗时。无领域决定或 ADR 改动；整片尚需主任务的最终统一退出。

后续 protocol probe 在本地复现 parent 默认 TextDecoder 会移除 stdout BOM，将污染的 ok:false 应答当普通拒绝；TS batch input decoder 同样接受带 BOM 的请求。两个极小公开工具边界回归均先 red，随后只把两个私有 control decoder 设为 ignoreBOM:true，使 BOM 原样进入 JSON.parse 并 hardfail；base64 产品正文与所有机器 Schema 保持原样。故障测试的 finally 同时保证这种未来 red 不遗留 child。修正后 make lint、16 个 lifecycle tests、相同前向 / 反序完整合同 suites 全部通过，表中更新为 BOM 修复后的实际计时；先前计时不作为最终收益数字。Go race 已在本架构 Go 最终源码通过，BOM 修正只涉及私有 JS / TS 工具及测试。
