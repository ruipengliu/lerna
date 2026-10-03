# 01 · 共同命令与机器契约

Status: ready-for-agent
Phase: A
Implementation: completed
Depends on: 无

## Problem Statement

应用和组件开发者目前只有行为文档，无法通过同版类型、请求验证和正反例判断实现是否兼容；成功回执也容易被误读成 Task 完成。

## Solution

交付可执行的最小合同包、Go / TypeScript 类型和合同测试入口。调用方能构造准确命令、识别固定回执、查询原身份，并对未实现方法得到明确拒绝。

## User Stories

1. As an application developer, I want stable command identities, so that reconnecting does not create another request.
2. As a component author, I want closed method schemas, so that invalid inputs fail consistently.
3. As a TypeScript developer, I want exact integer encodings, so that revisions and money do not lose precision.
4. As an application developer, I want distinct receipt and progress types, so that acceptance is not mistaken for completion.
5. As an operator, I want authenticated owner routing, so that requests cannot silently move to another authority.
6. As a component author, I want shared positive and negative fixtures, so that I can prove compatibility independently.
7. As a tenant administrator, I want authenticated subjects, so that payloads cannot impersonate another tenant.
8. As an SDK maintainer, I want version negotiation, so that an upgrade cannot silently change semantics.

## Implementation Decisions

- 沿用单 Go module 与 Go / TypeScript 同版合同，建立可运行构建及测试入口；工具链版本在实现时测试并锁定。
- 定义 Command 信封、OwnerRef、ObjectRef、ContentRef、Revision、Amount、CollectionView、CommandReceipt 和公共错误；整数采用十进制字符串，金额带单位，不用浮点累计。
- 规范化摘要覆盖方法、目标、认证主体绑定、payload、expected_revision、accept_before 与合同版本；排除 trace、连接和发送次数，不改业务含义。
- 采用严格 JSON Schema，拒绝重复键、未知字段和枚举、非有限数值、越界大小；设计占位版本不等于正式发布版本。
- 首个可执行范围是共同信封、command.get 合同与夹具；后续切片增加方法时同步补齐输入输出、状态前提、错误、类型和正反例。未完整实现的方法不得广告为已支持。
- 认证上下文提供租户、主体和委托链；payload 不得覆盖。查询不得触发模型、业务动作或安装。
- accepted、applied、rejected 是固定接纳事实；commit_unknown 属于传输结果，not_found 不证明其他 owner 未处理。

## Testing Decisions

边界为 Application / Component 的编解码与公开合同验证入口。实施前仓库没有产品测试；本切片建立后续复用的夹具与运行器，暂不声称持久去重已实现。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. Go / TypeScript 往返保留准确 ID、金额、时间精度和大整数；同版规范化摘要夹具一致。
2. 未知字段、重复键、非法枚举、过大正文与错误版本均拒绝；合法边界值可解析。
3. 改变业务参数、主体、期限或预期修订会改变摘要；只改 trace 或连接不会。
4. 回执类型不将 accepted 解释为 Task succeeded；查询可区分不可用、未找到和已清理。
5. 不同认证租户不能借 payload 或 target 冒用身份；拒绝视图不泄露对象存在性。
6. 未开放方法明确返回不支持；支持列表、Schema 摘要、类型与夹具版本一致。

## Out of Scope

- 真实数据库去重、网络传输和 Task 推进。
- 一次定义全部可选 profile，或宣称占位版本已兼容。

## Further Notes

这是首个可开始的切片。机器合同随具体方法逐片扩展，不要求先定义未来全部能力。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[contracts](../../docs/architecture/contracts.md#命令信封)、[contracts](../../docs/architecture/contracts.md#回执查询与错误)、[data-model](../../docs/architecture/data-model.md#公共类型)、[architecture](../../docs/architecture/architecture.md#代码依赖与运行调用)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。

## 切片退出证据（2026-10-03）

原六项合同任务及后置架构任务 07 均已 resolved；两轴发现由同一实施者修复，后续独立 Standards / Spec 审查均无新增发现。实现代码固定为 `23bac17ba0909c7a4d49d846eb08bc63391b99f0`。合同准确版本为 **1.0.0**，仅开放 **command / command.get**，生成器版本为 **1.0.0**。后续新增公共方法或字段须发布新准确版本，不原地扩大此版本；内部 Host 演示不属于其开放方法。

环境与命令：Go 1.27.1、Node 24.19.0、pnpm 12.8.1 / TypeScript 7.0.2；从锁文件执行 `make bootstrap`，`make check` 与受影响 Go `make test-race` 全部通过，生成零差异、`git diff --check` 通过。CI 格式发现使用必要 Git，14 个真实无 rg / 文件名 / 未跟踪文件等 probes 通过。新提交的远端 CI 结果与准确 SHA 见 [CI 核验](ci-verification.md)。

| 本片验收条件 | 实际证据 |
| --- | --- |
| 1 准确类型与摘要往返 | 158 个共同编解码夹具（68 个正例）前向 / 反序完整语料；每个正例实际 Go→TS 和 TS→Go typed codec 往返。46 个命令摘要案例含 31 个独立黄金预期；准确 ID、六微秒 UTC、大整数和金额不经 number 中转。另有合法 1 MiB 混合 Unicode / HTML 文本双向回归。 |
| 2 严格拒绝及合法边界 | 两端拒绝未知字段、重复解码键、非法 UTF-8 / surrogate / BOM、数字 token、枚举、错误版本、超限及深度；合法边界接受。13 项生成器拒绝、8 项 Schema 变更探针；公开 Schema 首次编译前不可变。 |
| 3 摘要绑定 | 同一独立命令 goldens 覆盖业务、主体、期限、预期修订变化及 trace / 编码重传不变；两种语言分别匹配独立预期，不用彼此一致代替真值。 |
| 4 回执与查询 | 固定 accepted / applied / rejected 与独立 progress；found / unavailable / not_found / gone 保留准确原引用，commit_unknown 属于传输。双方查询及 44 项响应夹具验证引用、修订和只读观察。 |
| 5 受信身份与 owner | 28 项共同受信读取场景加公开行为测试覆盖越权存在 / 不存在等价拒绝、跨租户、委托、payload 伪造、owner 错配 / 接替、别名隔离及有限取消；无默认 owner 回退。 |
| 6 准确协商 | 15 项协商案例和两个独立 Python Schema 黄金摘要；完整已登记方法才广告，未知版本 / 未开放方法 / 摘要错配明确拒绝。协商→构造→受信读取→响应解码正常路径通过；新增可选字段与深层规则变化均改变摘要。 |

完整本地 check 包含全部 Go suites、34 个 TS tests、16 项 runner 生命周期正常 / 故障测试、上述语料与生成 / 构建门槛。原代码发现与修复闭环见 [两轴审查](code-review.md)，架构决定、删除测试、真实两语言 adapter 及同负载实测见 [架构审查](architecture-review.md)。单次前向 119.217268 秒→10.909816 秒的本机测量只证明重复初始化成本减少；额外反序与生命周期成本单列，不代表生产性能。

退出范围只包括机器合同、注入身份 / 目录 / 事实源的受信读取及有限验证设施。尚无真实数据库去重、持久事实查询、网络认证、完整 Application SDK 或独立业务组件替换；F02 仅取得摘要变化证据，持久原键冲突由 02 验证，G3 的第二独立组件仍由 15 验证。所有这些边界保持未验收，不以本片绿色替代。
