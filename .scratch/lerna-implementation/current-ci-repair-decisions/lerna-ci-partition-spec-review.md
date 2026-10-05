# Spec — current CI Content partition

固定 baseline `75f0429d709d95b6a63137c397fcd9d80203ed5f`，HEAD `fa4ad6acfa58927000c830619da55598a0f9a506`。命令：`git diff 75f0429d709d95b6a63137c397fcd9d80203ed5f...HEAD`。36 个非空差异路径；3 个产品/测试文件，其余为机械证据。提交：`fa4ad6a Merge integration checkpoint for CI partition review`；`2a15405 fix: split discovered Content integration checks into three groups`。

独立 Spec 静态轴；来源 `/workspace/lerna/.scratch/lerna-implementation/issues/current-ci-partition.md`（issue）及 `/workspace/lerna/.scratch/lerna-04-content-snapshots/current-ci-7f656d9/adopted-partition-decision.md`（decision）。未查看另一轴，未运行 Go/Node/fmt/DB，结束时 HEAD 未变、工作树干净。

**代码 findings：0。** 未发现缺失的实现、范围扩张或看似实现但错误的行为。共享脚本只将既有 Content 分配块改为发现顺序 `index % 3`，依次运行 a/b/c；保留动态发现、closure/durable/other、精确锚定与字面转义、重复/空总量/非法/失败发现拒绝、空子组不执行、模式 flags、原 count1/p1/120s 及 set-e 失败传播。符合 issue13“仅 two-group block 改为三组……”与 decision15“Preserve immediate nonzero propagation and no later-group execution after failure”。没有改业务测试、权限、lease、deadline、固定夹具或公开合同。

完整阅读机械控制源码和当前 nine-controls raw：9 个具名 PASS、exit0/groupAbsent/noTimeout；固定 97 项和三组 literal oracle、small 0/1/2/3/4 inventories 两模式、Unicode/精确转义、Example/Fuzz、空组及第三组 status43 后无 durable/other 调用均有相应断言。对 783 行声明清单作完整机器核验：97 个源文件/声明行 pin 与候选源码一致，97 唯一名字及三个独立 literal 集合精确为 33/32/32。此为静态/外部工具机械证据，不冒真实 Go discovery 或 PG 行为。

**未完成的验收：1 项，已明确记录，非新代码缺陷。** issue14要求“实际 Go discovery 与共享普通/竞态入口全量覆盖，准确源码新 CI 完成”；decision23要求“Then exercise the shared normal and race entry … New CI must qualify the new tool pin”。当前这些尚未执行；完整 Recovery/fixture 与旧未运行竞态尾不能继承为通过。独立审查、源/进程/资源边界整合仍按 issue15完成后才交付。当前文档保持 claimed，未假报 resolved；三组是否满足真实 120s 仍须实际资格，失败后按原决定保留并停止，不能盲加组或重试。
