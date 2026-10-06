# 21: 本地档持久性验证

**What to build:** 在一个开发平台组合上验证本地档：进程崩溃、重启和掉电（或等价的存储故障模型）后，已确认的记录不丢失。结果写入平台准入表。

**Blocked by:** 03（持久化点崩溃钩子与重启）

**Status:** resolved

- [x] 选定一个开发平台组合（操作系统、文件系统、SQLite 版本和设置），写明理由
- [x] 掉电或等价存储故障模型的测试方法可重复
- [x] 确认之后的记录在故障后全部可读（标注 G3）
- [x] 结果和限制登记到平台准入表；未验证的组合不宣称支持

## 验收证据

- 选择当前开发宿主 macOS 27.0.1 / 26A434、Darwin 27.0.0、arm64、APFS，以便可重复运行真实原生屏障。固定生产驱动 SQLite 3.53.4 的 source ID 与编译选项摘要，WAL/FULL/fullfsync=ON；每条写连接核验实际组合，未知组合在关键事实写入前拒绝打开。
- `go test -tags fault ./conformance/fault -run '^TestStorage' -count=1 -v`：已有库 980 个恢复镜像，首次建库 1370 个镜像；各 9 个独立确认。全 I/O 前缀覆盖未同步丢失、逆序、交替子集、部分写入，以及检查点和 WAL 复用。
- 独立父进程保存 SUBMITTED、CLAIM、DECIDED 回执；恢复通过公共契约检查原身份、正文来源、任务与会话、不可变领取、原身份重试，以及 SQLite 完整性和外键。
- NORMAL、OFF、跳过同步的负对照确定丢失已确认命令。内容和接纳提交的同步错误均拒绝确认；真实 F_FULLFSYNC 失败由生产严格包装器拒绝回退；目录 fsync 失败拒绝打开。临时移除生产拒绝检查会使测试报 `sync error leaked receipt`，恢复检查后通过。
- 最终默认 `TMPDIR` 下 `make check` 全通过，包含 race、故障套件、依赖方向、规则编号、协议生成。另在 `/Volumes/Data` 的临时目录运行过完整检查。生产构建不含故障配置与 VFS 轨迹代码。
- [平台准入表](../../../docs/architecture/topics/deployment.md#11-本地档平台准入表)与[可复现方法和边界](../../../docs/architecture/verification/local-durability.md)记录有条件的等价模型资格。未做物理电源中断；介质永久丢失、文件适配器原语和未来 P5 出口必须单独验证，不由此推定支持。


## Answer

本地档资格继续限定于原平台准入表中的 Darwin arm64、APFS、实际 SQLite 版本和严格 WAL/FULL/fullfsync 配置。原完整 I/O 前缀、首次建库、P4／出口联合矩阵和负对照以当前完整重跑的原身份回执与父测试断言重新绑定；没有独立 t.Run 的实际循环保持其源代码和父测试联合证据，不发明逐映像行。未执行物理电源中断，不外推未验证平台、永久介质丢失或生产容量。

当前逐项语义见 [ROOT 当前验收认定](/Volumes/Data/proj/lerna-m1-context/ticket23-root-standards-fix-current-specific-semantic-qualified-01.json)，绑定 TESTED `8d951ad2d89d06282560280666ca162c957e9e68`；修复提交 `94f77d187e0b65010c4332302beb93854674fedb` 已经独立合并并经 [ROOT POST](/Volumes/Data/proj/lerna-m1-context/ticket23-root-standards-fix-formal-merge-independent-audit-01.json) 核验。此处记录本地 Markdown 收尾；该收尾提交的独立合并 POST 和工作树清理仍待完成。
