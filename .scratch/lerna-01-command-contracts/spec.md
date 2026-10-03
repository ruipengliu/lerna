# 01 · 共同命令与机器契约

Status: ready-for-agent
Phase: A
Implementation: in-progress
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
