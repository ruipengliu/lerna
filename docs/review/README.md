# 第四轮架构评审

| 日期 | 修订说明 |
| --- | --- |
| 2026-10-04 | 以 `0a21f22` 为基线复核第三轮调整；归档第三轮文件，提出五项建议。 |

- 状态：评审建议，未采纳；本轮没有处理决定文件
- 读者：架构评审者、后续设计修订者
- 范围：`docs/architecture` 的设计契约及其跨模块影响
- 规范：项目目标、分层与模块、ADR、模块设计；写作遵循文档规范

本轮共 5 项意见，2 项 P1、3 项 P2。建议保留现有模块划分、同用户裁决事务和 R7 交接。主要缺口在于：释放控制、建立新责任、证明旧责任结束，这三件事仍有未对齐的前提。

所有反例都是对基线文档的事件顺序推导。它们不是运行缺陷报告，也不是测试已通过的声明。各篇分别列出文档事实、推断、修改方案、修改落点和验收场景。`docs/research` 没有作为规范或证据引用；本轮结论依赖仓库内的契约对照，没有新增外部事实引用，也没有联网。

## 1 评审意见

| 编号 | 优先级 | 必须解决的阶段 | 结论与建议 |
| --- | --- | --- | --- |
| R4-01 | P1 | M1 开放核验拒绝后继续执行前 | [释放冻结与旧动作封闭](replanning-before-seal-confirmation.md)：保留拒绝时释放冻结，但把旧清单封闭尚未确认列为独立等待，阻止旧动作与替代动作并行。 |
| R4-02 | P1 | M4 开放内部委派前 | [父任务完成与子任务收尾](parent-completion-child-closure.md)：先等待全部子任务关闭并取得递归收尾依据，再启动父核验，避免范围变化或相互等待。 |
| R4-03 | P2 | M1 单位置调用接口定稿前 | [模型调用的两种上限](model-call-limit-accounting.md)：逻辑位置与实际发送分别计数；安全重发仍消耗发送额度，恢复原结果不消耗。 |
| R4-04 | P2 | M2 开放记忆合并与纠正前 | [记忆替代的依赖类型](memory-replacement-dependency-types.md)：旧条目的明确比较读取使用核心绑定的历史视图，更新提交另外检查预期修订，避免新修订自失效。 |
| R4-05 | P2 | M1 收尾契约定稿前；M4 前复验 | [未发送动作的内部证明](internal-proof-for-unsent-operations.md)：原执行管理负责方可以证明出口从未开放且已封闭；发生过发送风险后继续要求外部原始观察与受信解释。 |

P1 表示对应能力开放前必须优先关闭的执行或完成一致性缺口。P2 表示实现前必须统一的契约、类型或计数规则。M4 的问题不阻塞未开放委派的 M1。

## 2 第三轮调整复核

先读[第三轮处理记录](archive/round-3/disposition.md)，再按实际采纳方案核对；没有把第三轮原意见中未采纳的实现方式当成现行要求。

| 第三轮调整 | 本轮结论 |
| --- | --- |
| R3-01 条件集绑定已处理输入版本 | 任务编排 2.2、4.2、会话 4.1、核心契约 2.5 和推理快照已形成共同约束。`DRAFT` 或绑定落后时不能推进普通目标或成功关闭；准备工作的用途由核心指定。本轮没有足够依据另列新问题。 |
| R3-02 `REJECTED` 总是释放冻结 | 冻结归属与三个出口的释放规则已写入。保留此简化。封闭尚未确认时的替代动作窗口见 R4-01；不要求恢复拒绝后持有冻结。 |
| R3-03 P4 检查祖先控制代次 | 父修改、暂停、取消对旧子动作的检查已写入；跨裁决域内部委派已明确不支持。正常完成时怎样固定子任务后果仍缺具体协议，见 R4-02。 |
| R3-04 提议请求加调用位置 | 原结果恢复、不同位置独立采样、同位置异描述冲突、结果不可恢复均有规则。不重复恢复身份问题。位置上限与实际发送上限混用见 R4-03。 |
| R3-05 有类型的记忆依赖 | 当前依据检查已覆盖读取、模型发送、发布、准入、P4 和完成核验。默认记忆更新自身也读取旧修订，这条路径的类型未指定，见 R4-04。 |
| R3-06 执行证据信任边界 | 可信出口固定观察、普通插件仅解释、受信规则判定终局的边界已落实到文档。内部未发送证明与“只能使用外部观察”的措辞冲突，见 R4-05。 |

“已写入”只表示文档一致性核对，不表示实现或运行验收完成。前三轮已经列为阶段前置条件的云端接管、平台持久性、扩展兼容证据和跨权威域使用顺序，本轮不重复立项。

## 3 整体修改方向

现有事实归属没有需要推翻的证据。建议让关键内部判定返回足以支持下一步的具体依据，减少只检查状态名的分歧：

- 任务编排释放冻结时，保留准确的旧责任等待；准备下一次提议与允许新的目标动作分别判断。
- 父任务通过子任务的关闭及递归收尾依据固定范围。M4 先不支持父核验与运行中的子任务并行。
- 模型请求持有逻辑位置关联；执行管理保存物理发送身份；预算持有发送次数和金额的占用。恢复只查询已有身份。
- 内容模块分别检查更新提交的预期修订、持续的当前事实依赖，以及历史比较来源的治理限制。
- 执行管理按证据来源区分内部封闭证明与外部终局证明；两者都有准确范围，插件不能选择更宽松的路径。

这些调整使用现有事务域和写入方，不增加数据库或规则服务。尤其不能用“都调用一个检查入口”替代跨域封闭回执；R7 的交接责任仍然保留。

建议落实顺序：M1 先统一 R4-05 的封闭证明，再完成 R4-01 的等待与继续规则，同时固定 R4-03 的计数契约；M2 完成 R4-04；M4 完成 R4-02，并复验第三方插件无法伪造两类证明。

## 4 归档与历史链接

第三轮以下 8 个文件已移动到 `archive/round-3/`。正文保持原样，只调整链接地址：

| 原文件名 | 归档位置 |
| --- | --- |
| `README.md` | [第三轮总览](archive/round-3/README.md) |
| `disposition.md` | [第三轮处理记录](archive/round-3/disposition.md) |
| `input-resolution-before-progress.md` | [新输入处理门禁](archive/round-3/input-resolution-before-progress.md) |
| `rejected-round-freeze-release.md` | [被拒绝轮次的冻结释放](archive/round-3/rejected-round-freeze-release.md) |
| `ancestor-control-at-egress.md` | [祖先控制进入 P4](archive/round-3/ancestor-control-at-egress.md) |
| `model-call-replay-identity.md` | [模型调用的恢复身份](archive/round-3/model-call-replay-identity.md) |
| `memory-correction-use-barrier.md` | [记忆纠正后的使用检查](archive/round-3/memory-correction-use-barrier.md) |
| `execution-evidence-trust-boundary.md` | [执行证据的信任边界](archive/round-3/execution-evidence-trust-boundary.md) |

第一、二轮继续保留在 [round-1](archive/round-1/README.md)、[round-2](archive/round-2/README.md)。第三轮内部同目录引用继续指向第三轮；对第一、二轮及架构文档的引用按新位置改址。

现行架构的 14 个文件共有 17 处第三轮处理记录链接改址：

| 文件 | 改址数量 |
| --- | ---: |
| [layers.md](../architecture/layers.md) | 1 |
| [overview.md](../architecture/overview.md) | 4 |
| [core/contracts/README.md](../architecture/core/contracts/README.md) | 1 |
| [core/tasks/README.md](../architecture/core/tasks/README.md) | 1 |
| [core/sessions/README.md](../architecture/core/sessions/README.md) | 1 |
| [core/grants/README.md](../architecture/core/grants/README.md) | 1 |
| [core/ledger/README.md](../architecture/core/ledger/README.md) | 1 |
| [core/egress/README.md](../architecture/core/egress/README.md) | 1 |
| [core/content/README.md](../architecture/core/content/README.md) | 1 |
| [ports/reasoner/README.md](../architecture/ports/reasoner/README.md) | 1 |
| [ports/reasoner/default.md](../architecture/ports/reasoner/default.md) | 1 |
| [ports/memory/README.md](../architecture/ports/memory/README.md) | 1 |
| [ports/executor/README.md](../architecture/ports/executor/README.md) | 1 |
| [platform/extensions/README.md](../architecture/platform/extensions/README.md) | 1 |

上述链接从相应层级的 `review/disposition.md` 改为 `review/archive/round-3/disposition.md`。`docs/adr/` 已检查，没有指向此次移动文件的链接，因此未改动。

## 5 检查范围与限制

本轮仅修改 `docs/review/` 和上表中的历史链接地址。架构设计正文、ADR 内容、代码和其他工作区文件没有修改。没有安装软件、提交或推送。

检查结果：145 处相对链接的文件目标及适用的章节锚点均有效，包含新文档、第三轮归档和 17 处历史改址；8 个归档文件去除链接地址后与基线正文一致；架构差异只有获准的链接替换。`git diff --check` 通过，另行检查了尚未跟踪的新文档空白格式。文档检查不证明分布式正确性；各篇验收场景仍须在对应阶段实现并验证。
