# 7f656d9 的准确 CI 失败

推送 head `7f656d964a1f73aeb333ac1e1217be1ed405e4b8` 的运行 37275970295：合同与基础检查成功，真实集成普通模式成功，竞态模式失败。凭据已脱敏的原日志：[contracts](contracts.log)、[durable](durable.log)；[机器记录](observed.json)区分已执行和未执行部分。root 阅读相关检查／包汇总与故障段，未声称人工全文阅读 setup/container 日志。

实际发现 201 项：closure1、Content97、durable60、other43。普通 Content 两组49／48耗时60.086／94.440s；竞态第一组75.024s，第二组达到120.039s总包超时。栈在实际 CopyToSecondary → SaveSecondaryCopy，`(0s)`不证明该测试自身期限失败。竞态 durable／other／fixture尾未执行，不继承普通模式结果。

[已采用必要决定](adopted-partition-decision.md)将 Content 按发现顺序轮转到固定三组，保持逐项恰好一次、原模式／期限及立即失败约束。97 项 literal 预期的机械首 RED 真实失败；最小脚本候选同测试真实 GREEN。它仍未证明真实数据库耗时或新 CI 成功；第三组错误与边界控制、真实当前发现和共享集成仍待验证。
