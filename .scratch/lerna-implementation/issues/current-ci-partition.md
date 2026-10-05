---
status: claimed
labels: [ready-for-agent]
---

# 让完整 Content 竞态分组在原有限期限内执行

准确 `7f656d9` 的 CI [37275970295](https://github.com/ruipengliu/lerna/actions/runs/37275970295) 基础和普通集成通过，竞态 `content_a` 49 项 75.024s 通过，`content_b` 48 项在整包 120.039s 超时。运行中的用例标 `(0s)`，实际栈在 CopyToSecondary/SaveSecondaryCopy；这不是该用例自身 20s 截止失败的证明。后续 durable/other/fixture race 未执行。

按已采用必要决定，最小候选是固定发现序轮转三组，当前 97 项变为 33/32/32。不预言已足够，不增加原 120s，不改变业务期限和测试。来源是切片 02 的有限持久工作与 03 的完整有限故障要求，外部 Go-tool 机械 seam 已授权。

- [ ] 最先用固定来源的 97 项和独立 literal 三组 oracle 跑实际机械选择反例；明确不是新的真实 Go discovery/PG 资格。
- [ ] 仅 two-group block 改为三组，保动态 Test/Example/Fuzz、Unicode/精确转义、重复/空/损坏发现拒绝、空子组不运行、exact-once、normal/race、count1/p1/120 和失败即停止。
- [ ] 更新有限边界/第三组失败及原外部工具控制，实际 Go discovery 与共享普通/竞态入口全量覆盖，准确源码新 CI 完成。
- [ ] 独立 Standards/Spec、实际源/进程/资源边界证据与正常整合后交付；候选再次失败则保留准确失败，不盲增分组或重试。

独立 WT `/tmp/lerna-worktrees/current-ci-partition`，初始准确基线 7f656d9。03、取消处理和 06 不增加新依赖。历史超时进程关闭和清理不据新通过补证。
