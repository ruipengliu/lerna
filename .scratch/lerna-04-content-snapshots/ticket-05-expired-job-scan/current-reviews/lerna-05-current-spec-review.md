# Spec review — ticket 05 current scan candidate

固定 baseline `1a4e1d2d704261a59491dfc6e0c5d26b80ec5107`，HEAD `8db74e3dfd2677adb37fbdd13bd93e76b61b5eb7`。命令：`git diff 1a4e1d2d704261a59491dfc6e0c5d26b80ec5107...8db74e3dfd2677adb37fbdd13bd93e76b61b5eb7`；14 个非空差异文件。提交：`85a1067 test(content): expose later publication blocked by expired cleanup page`；`8db74e3 fix(content): select executable work before bounded consumer pages`。

审查来源：`.scratch/lerna-04-content-snapshots/spec.md`、`issues/05-body-cleanup-and-holders.md` 七 AC、`ticket-05-api-handoff.md`。独立 Spec 轴，仅静态阅读；未修改源码或运行 Go/PG。

结论：缺失/部分要求 0；未要求的范围扩张 0；实现错误 0。此结论针对当前差量，不宣布七 AC 或整个切片已正式接受。

票据第 13 行要求“不同新version不受旧清理误删；源政策收紧传播完整有限分页”。Service 在 LIMIT 前分别选择 publish/policy，Manager 仅选择 policy；Lifecycle 在 LIMIT 前按完整保存 Subject、原 primary 和可执行 seal 窗口选择，解决原 64 条到期清理责任遮挡后续版本及消费者的问题。六产品改动保留原锁后完整身份、fresh Clock、Claim、原责任 Deadline、物理 fence 和全部 holder ACK；没有将预选结果当作擦除证明，没有修改 runtime Scan、原 SQL 或旧 Job 责任。

票据第 14 行要求“不扫描他人scope”。完整 delegation chain 参加比较；SQL 对合法其他 Subject/primary 排除，对缺失、形状、整数解码、持久 shadow 或 canonical seal 不合格保留错误候选。PG 转换失败仍返回错误；selected 非法 Record 再经 Go 解码保留具体原因。独立检查未发现把坏行 COALESCE 为合法他人 scope 或空页的路径。时间预选的 1µs 容差只扩大候选集合，锁后原 Go 时间门仍决定执行。

票据第 15 行要求“正常/失败/重开通过Content、受信holder和独立字节观察验收”。完整阅读四新增测试及八份 normal/race 原日志：原 full64、Manager、不同完整 Subject 和 Host 坏 deadline/合法特殊字符对照均有真实 PASS/exit0 ACK。业务测试核完整原责任、准确字节及重开历史；Host 私表操作明确只用于故障安装/恢复，不作业务成功 oracle。迁移测试修正为实际三版本及全部冻结 SHA，未放松原 checksum 拒绝。

检查了 final27 汇总及澄清 sidecar 的实际完成字段，未独立审计全部资源台账，不将 group absence 当 logical Close；旧 killed-holder unknown、准确新 CI、资源审计与 root 正式接受仍按 handoff 保留。
