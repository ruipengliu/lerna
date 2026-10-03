# 16 · 版本激活、停用与在途恢复

Status: ready-for-agent
Phase: D
Implementation: not-started
Depends on: [13](../lerna-13-distributed-runtime/spec.md)、[15](../lerna-15-component-interoperability/spec.md)

## Problem Statement

组件升级可能改变新任务默认版本，也可能破坏旧操作的查询和收尾；安装完成、发布批准和实例可服务资格不能被合并成一个开关。

## Solution

交付 extensions.prepare / activate / deactivate / get 与准确版本管理。新任务采用当前默认版本，旧 holder 在有效资格下继续；显式停用封新调用，回退和卸载保留原效果与费用责任。

## User Stories

1. As an operator, I want immutable installation locks, so that a version identifies exact artifacts and dependencies.
2. As a release approver, I want approval separate from readiness, so that installation does not authorize use.
3. As a user, I want in-flight versions pinned, so that upgrading cannot reinterpret my current task.
4. As an operator, I want activation races rejected, so that late initialization cannot replace a newer instance.
5. As a security operator, I want deactivation to stop new calls, so that old tasks cannot bypass a safety stop.
6. As an operator, I want qualified rollback, so that an expired approval is not silently revived.
7. As a user, I want old effects queryable, so that stopping a component does not erase responsibilities.
8. As an operator, I want complete holder checks before uninstall, so that cleanup cannot remove an in-use implementation.
9. As a maintainer, I want data migrations recoverable, so that rollback does not reverse permissions or spending.

## Implementation Decisions

- InstallLock 固定制品、依赖、配置、profile、数据格式和平台；ReleaseApproval、InstanceReadiness、Activation 分别保存，准确任务和操作持有对应 holder。
- 稳定部署目标只有一个 BindingHead；activate 比较 target revision / generation 后提交新头和准确就绪。旧初始化只能更新自己实例，旧清理不得删除新实例。
- 重启建立新随机 instance_id 并重新自检。构造和自检不得隐式发起真实模型或目标调用；第三方代码放受控进程、沙箱或远程服务，不依赖 Go 热卸载。
- 普通升级仅改变新任务默认选择；旧 Task 可在原 Activation、批准、就绪和 holder 有效时继续下一轮，仍逐次执行授权预算检查。
- 显式 deactivate 封闭该 Activation 全部新调用，包括旧 Task 的新一轮；原查询和结算由可信旧实现或合同验证接管器承担。没有合格接管器时保持等待与限制。
- 迁移必须具备兼容声明、原责任 checkpoint、迁移记录和失败恢复路径；自动回退是新激活，需要当前有效旧批准、格式兼容与目标代次比较。
- 数据库迁移采用扩展、分批、切换、观察、收缩，回退不倒退效果、撤权或费用；旧格式删除等旧 holder 关闭及回退期结束。
- 卸载先封新引用并检查完整 holder 集合，不能只查一页；残留分别显示负责方及状态，不冒充 disposed。

## Testing Decisions

通过公开宿主管理合同驱动版本变化，Application / Component 查询验证对在途工作的影响。复用 13–15 的真实远程组件、故障和合同套件；不以内部指针是否变化判断升级正确。

测试只断言公开行为、持久恢复结果和独立目标状态；不依赖私有表布局或内部调用次数。每个故障或拒绝案例必须配允许正常完成的对照。

验收条件：

1. 并发激活 A、B 且 A 自检迟到，只有匹配目标代次的结果可更新默认头，旧清理不影响当前实例。
2. 安装完成但无批准或无当前实例就绪时不可调用；重启不能继承旧就绪。
3. 普通升级后新 Task 选新版，原 holder 的旧 Task 在资格有效时仍可新一轮调用；显式停用后两者不能再进入旧 Activation。
4. 安全停用后原效果和费用可由可信实现核对；无接管器时显示未结而不运行已知不可信代码。
5. 旧批准过期、格式不兼容或第三版已激活时，迟到回退拒绝，不自动刷新代次强覆盖。
6. 迁移中断可按原记录恢复，已发生效果、撤权及累计费用不因回退倒退。
7. 超过一页的 holder 仍阻止卸载；全部关闭并清理完成后才允许报告已处置，正常升级对照也能完成。

## Out of Scope

- 未经验证的自动 Task 迁移、Go 进程内代码热卸载。
- 策略质量改善自动批准；该流程由 21 承接。

## Further Notes

早期静态装配在 07 已固定准确版本与启动资格，本切片扩展为可操作的完整生命周期，不把首版门禁推迟到此时才建立。

依赖项表示实现先决条件；`ready-for-agent` 表示规格已明确，不表示依赖已完成或能力已开放。全部验收通过并附准确版本、环境、命令、结果和限制后，才可将本切片记为完成。共同执行与证据规则见[切片索引](../lerna-implementation/README.md)。

依据：[governance](../../docs/architecture/governance.md#插件从安装到可服务)、[governance](../../docs/architecture/governance.md#在途版本升级与回退)、[contracts](../../docs/architecture/contracts.md#授权和宿主管理方法)、[ADR-0009](../../docs/adr/0009-versioned-activation-and-recovery.md)、[ADR-0006](../../docs/adr/0006-authorization-budget-at-action-boundaries.md)。
