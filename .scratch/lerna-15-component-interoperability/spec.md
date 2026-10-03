# 15 · 三系统第二实现与异构互操作

Status: ready-for-agent
Phase: D
Implementation: not-started
Depends on: [10](../lerna-10-model-search-adapters/spec.md)、[12](../lerna-12-research-report-app/spec.md)、[13](../lerna-13-distributed-runtime/spec.md)、[14](../lerna-14-memory-core/spec.md)

## Problem Statement

只有默认实现自测不能证明框架可替换。组件开发者需要在不访问默认私有表的情况下实现合同，并在不同语言与真实网络下通过相同反例。

## Solution

为 Decision Engine、Memory、Executor 各提供第二个独立实现，至少一个采用 TypeScript 经真实 gRPC 或 WSS 运行；同一版本合同套件验证接纳、权限、恢复、费用和关闭语义。

## User Stories

1. As a component developer, I want implementation-independent contracts, so that I can replace a default component.
2. As an application developer, I want equivalent task semantics after replacement, so that integrations do not need internal changes.
3. As a TypeScript author, I want real remote interoperability, so that compatibility is not limited to Go processes.
4. As a reviewer, I want independent state ownership, so that a wrapper around the default implementation does not count as replacement.
5. As a user, I want replacement to preserve permission checks, so that customization cannot bypass control.
6. As an operator, I want unknown results recoverable, so that remote failure does not invent a second action.
7. As a component developer, I want precise errors and limits, so that unsupported guarantees are visible.
8. As a maintainer, I want reader feedback from a real implementer, so that undocumented assumptions are discovered.

## Implementation Decisions

- 固定测试矩阵：默认模型决策与独立规则决策、默认词法 Memory 与独立朴素匹配 Memory、默认受管文件 Executor 与独立受管目标 Executor；后者各自接纳并持久保存本模块权威记录。
- 至少一个第二实现采用 TypeScript 并走真实网络。只改模型名称、包名或同进程代理默认实现不能作为第二实现证据。
- 第二实现可采用不同持久引擎，不强制使用内部 Host；不得调用默认组件私有存储或共享其业务决定。
- 同版 Component 套件覆盖 Schema、前态、原身份、方法成功点、当前权限、取消、查询恢复、累计费用、准确 ContentRef 与关闭。
- 在装配时选择准确 ComponentRef / Binding，复跑 Application 完整任务；动态升级、旧任务版本迁移和卸载由 16 管理。
- 检索排序算法允许不同，但来源、权限、版本和缺口语义必须一致；决策内容可不同，但不能越权或自己写 Task 终态。
- 至少一名组件开发者仅凭合同文档实现或接入一个组件，记录缺失规则、追问及修订；发布接口、Schema、SDK、测试同版。

## Testing Decisions

Component 公开合同为主，Application 完整路径作替换后整体验收。复用 01、03、07、10、13、14 的同版套件；各实现运行同一输入与故障案例，避免复制私有单元测试。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 三类组件各有两种独立实现通过支持范围内的同一合同套件，报告准确实现和合同版本。
2. 至少一个异构语言实现经真实 WSS / gRPC 完成接纳、查询、取消和故障恢复；不以序列化单测替代。
3. 切换组件后同一应用无需读取默认内部存储，仍可完成正常报告流程和三种核心中断。
4. 组件被杀、答复丢失或乱序时按原身份恢复；unknown、未结费用和取消限制不被包装成成功。
5. 所有实现拒绝跨租户、旧授权和未知 Schema，合法许可路径仍成功。
6. 第二 Memory 在获准集合上排序，撤权后的分页失效与默认实现一致；无需强求不同算法分值完全一致。
7. 真实组件开发者完成读者试验并记录发现；仅框架作者自行调用不能替代该证据。

## Out of Scope

- 要求第三方内部代码结构一致、算法输出逐字相同。
- 动态热卸载和自动迁移在途 Task。

## Further Notes

D 的“三系统第二实现”至此闭合。14 的最小 Memory 为必要前置；E 的自动提取与委派不是本切片的隐藏依赖。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[validation](../../docs/architecture/validation.md#组件合同与互操作)、[validation](../../docs/architecture/validation.md#文档与真实读者验收)、[architecture](../../docs/architecture/architecture.md#哪些接口公开)、[contracts](../../docs/architecture/contracts.md#三种调用面)、[ADR-0002](../../docs/adr/0002-stable-kernel-replaceable-strategies.md)、[ADR-0007](../../docs/adr/0007-versioned-content-memory-snapshots.md)、[ADR-0008](../../docs/adr/0008-internal-contracts-external-protocol-adapters.md)。
