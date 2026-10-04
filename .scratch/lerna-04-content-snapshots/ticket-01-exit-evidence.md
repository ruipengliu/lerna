# 04 首票：耐久发布与准确读取退出记录

2026-10-04，首票8/8AC已接受并合入；切片04累计8/41，整片仍在实施。

- 交付：`1a7d910238eb74cddc712d92b0ba4014a72ff507`。受测可执行源码：`14ead831b8834892da5ff13a9b883494247f327f`。
- 注释资格：`4aceecefb45125597d45db75aa757fe1aab96eb9`只替换两行普通英文注释，所有其他文本/对象及可执行测试数据保持；不声称测试文件blob相等或重新运行。
- 合并：`eba167d65b7e96c1c81c5db9883d483a7e6d2e2d`，父提交`64c6872`与`1a7d910`，完整tree `fdd4bc562fb6dedde5e2f28701cdee4c383cf135`与worker相等，root独立核验。

[实际API及逐AC映射](ticket-01-api-handoff.md)覆盖隔离1.2合同、PG短Tx staging/receipt/Job、真实对象Sync与独立回读、双重身份、有限正文/range、当前准确权限、重开与无损旧reader、迁移和确切资源关闭。

最终受测14的完整locked check（session46700）实际native0/group absent：35工具测试、44TS、两generator、158旧1.0/89旧1.1/101新1.2共同夹具两序真实Go↔TS、format/vet/generated及Go/TS build。部分未变Go单元使用缓存，未改写为fresh运行。Get受影响15组race（session47356）22.361s，native0/group absent；前一完整Content受影响race40.887s及受保护旧路径验证分别保留其源码pin。

公开正常读回、真实PG锁等待、撤权、源政策、关联、错误完整target policy和授权次序均有正常对照及真实red→green。[详细证据](ticket-01-evidence.md)保留编译失败、预期修正、工具exit2和不充分中间green，不将机械pause/诊断当native存储故障。

最终[审查](ticket-01-code-review.md)分别为Standards0hard/1可选KEEP P3、Spec a0/b0/c0；原六项P2及注释P3已闭合。[架构源复核](ticket-01-architecture-final.md)关闭必要F1，无必要新增框架或ADR，旧报告保留原cutoff。

最新[有效资源审计](ticket-01-resource-audit-target-policy.json)353登记组、289确切schema、65原blockerPID、518对象/合同/生成目录全无live/residual；审计组2147781实际退出0并absent。原overlay ACK dev27/inode431292、日志/cache和clean worker工作树保留至整片安全清理；旧02/03未知scope未触碰，不声称全环境清零。原worker已明确释放唯一LOCAL构建/测试/数据库槽。

1.2完整profile仍不广告。完整来源闭包/传播、Snapshot、物理清理/holder、SIGKILL依原后票交付；不声称生产Grant、真实模型、断电/S3或跨owner原子保证。本检查点新push CI尚待按准确head核验，旧64文档CI不能证明新Content源码；首票独立出口不等待整片关闭。

2026-10-04，前述“准确新CI待核”为历史；root已按准确7c0bce5核run37207013464 completed success，两job全部steps及真实日志通过，详细[CI记录](ci-verification.md)。不据此关闭02或04整片。
