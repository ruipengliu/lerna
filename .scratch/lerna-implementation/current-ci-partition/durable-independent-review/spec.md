# Spec — Content3 / Durable2 finite integration partition

固定 baseline `b9db5cc67647a15d6556fe92941757034a1e80d1`，HEAD `d0fcc61264904070ce94e1146f07327a0ef9b2aa`。命令：`git diff b9db5cc67647a15d6556fe92941757034a1e80d1...d0fcc61264904070ce94e1146f07327a0ef9b2aa`。214 路径，4 个源码/测试文件；提交序列 `2a15405/fa4ad6a/860df74/4610928/4142a42/d0fcc61`，完整 SHA/parents/subjects 见 `/tmp/lerna-partition-durable-fixed-review/manifest.json`。

独立 Spec 静态轴；依据 WT `.scratch/lerna-implementation/issues/current-ci-partition.md`（issue）、`docs/agents/issue-tracker.md`、采用决定 `/tmp/lerna-ci-partition-durable-next-decision.md`（decision，SHA `4561f32c…`）。完整读取共享 shell 和机械测试，机器完整读取两份 literal JSON；不读取另一轴，不运行 Go/Node/fmt/PG，不修改产品。whole04 仅用于范围界定，此工具修复不承诺完成整片。

**代码 findings：0。** (a) 未发现缺失的实现；(b) 未发现未经要求的范围扩张；(c) 未发现错误实现。issue15 要求“仅 Content分组改为三组、Durable分组改为两组”；decision15 要求“两 explicit arrays and index%2…normal uses the identical partition”。脚本按原发现顺序 Content `%3` / Durable `%2` 顺序执行；closure/other、动态 Test/Example/Fuzz、benchmark 排除、Unicode/精确 RE2 转义/完整锚点、重复/非法/空总量拒绝、空子组无调用、原 flags 与 set-e 失败状态传播保持。固定 JSON 仅供测试 oracle，未替代真实动态发现；业务正文、合同、权限、租期或期限未改。

decision17 要求“independent literal expected selectors…explicit30/30 expectations, and A/B failure-stop controls”。静态全量核97/60各名字唯一，独立 literal 集合精确为33/32/32与30/30。原控制保留，新增0–3边界/Unicode/字面量、两模式60 exact-once、A47/B53停止控制；完整13控制原日志为13 PASS/exit0/noTimeout。此为机械资格，不冒 PG 或完整共享入口成功。

**已知未完成资格：1 组，非新代码缺陷。** issue16–17 要求“共享普通/竞态入口全量覆盖，准确源码新 CI 完成”及“实际源/进程/资源边界证据与正常整合后交付”；decision19 要求完整 Recovery/other/fixture 尾。新固定源码 wholeN/R、currentcheck、资源closure、准确整合push/newCI尚待。issue21及decision25诚实区分原aggregate1445与nominal1565，不续期、不保证各段耗满；旧竞态只158被实际选择，未执行other43/fixture不计PASS，历史UNKNOWN不补Close。issue保持claimed；候选失败须保原事实停止，不能盲增组或重试。

结束核验：固定 HEAD 未变，工作树干净。
