# 当前 CI 修复的只读决定与源审查

固定三组候选 fa4ad6a 对比 root75：Standards 0硬性／1可选P3，Spec 0代码发现。重复过滤表达式保留为当前非阻塞可读性建议；实际共享普通／竞态及新 CI 未完成，票不 resolve。两轴原文分别保存，不合并排序。

Run blocked SQL 首正常观察到了原SQL等待，随后 opaqueDSN装配被PoolScope拒绝；采用 Astra851a9 的仅测试修正，保持原同一 Store和可解析配置，明确准备与实际等待分别证明。原 firstfailure、期限和 UNKNOWN不改。这里没有新产品通过声明。

Run固定3570180对比root75的[Standards原报告](run-standards-review.md)：0硬性、1可选P3。两个Host测试的有限退出辅助形状重复，当前保留显式场景顺序；该可读性建议非阻塞。[Spec独立原报告](run-spec-review.md)代码缺漏／越界／错误均0，待执行资格单独保留。新PG竞态、原受影响套件、资源审计、实际交付仍待。
