# 调研静态验证记录

验证对象是本次五项目报告、共同索引/基线/优化分析，以及来源快照；验证通过不表示上游程序、本项目实现或候选优化已运行通过。

## 1. 可重复的检查

在仓库根目录运行：

```sh
python3 docs/research/agent-harness-comparison/verify-research.py --output docs/research/agent-harness-comparison/verification.json
```

[脚本](verify-research.py)只使用 Python 标准库和 Git 只读命令，不加载参考项目代码。机器结果保存到[verification.json](verification.json)；固定输入见[sources.json](sources.json)。

检查范围：五个 clone 的 origin/branch/commit/tree、工作树洁净与忽略状态；154 个基线文件 SHA-256；报告源码 URL 的固定提交、实际路径及行号界限；本地 Markdown 路径与标题/显式锚点；引用定义、代码围栏闭合、尾部空白与最终换行。Mermaid 只检查围栏，不宣称渲染器解析或视觉布局已验证。

## 2. 交叉复核范围

分项目研究分别核查入口、类型、写路径与调用时序，根侧归并并复核关键事实。Prime/Crush 另作静态交叉审阅；综合分析又核查多条运行主线、现行 24 项方向和默认物理请求次数等边界。

核查促成的实质修订包括：Pi 三条运行路径及 hook 重验证分开；Codex SQLite 投影与独立业务状态分开，普通追加与文件压缩同步路径分开；DeepSeek durable Goal/Schedule 与内存 jobs、合作 VM 与 OS 限制分开；Prime cron 锁/写失败降级明确，旧 snapshot 格式的恢复限额不泛化；Crush flush 失败及 MustDeliver 有界丢失未包装成耐久保证。源码路径/行号超界和本地锚点在静态检查中修复。

## 3. 结果与限制

2026-10-01 最终检查通过：五个参考快照均与清单一致、洁净且被忽略；154 个基线文件内容未变；9 份 Markdown 中的 414 处固定提交源码引用（397 个唯一定位）和 262 处本地链接通过，16 个 Mermaid 围栏闭合。完整统计与错误列表见[机器结果](verification.json)。

此记录及综合建议均为静态研究交付；没有安装上游依赖、运行测试/benchmark、启动模型/服务、执行隔离攻击或数据库故障实验。链接检查验证定位存在，不自动证明正文解释正确；语义由前述源码追踪及交叉复核判断。

本次没有修改比较基线文件；后续基线变化可通过重新运行脚本检测。运行实现、真实故障保证、质量/费用/时延改善和生产 RPO/RTO 需按[架构验收](../../architecture/.draft/validation/README.md)及[优化计划](../../architecture/.draft/validation/optimization-evidence.md)取得独立证据。
