# 18 skill-agent-configuration

Status: partial
Blocked by: 01, 05, 06, 08
Implementer: governance_impl

依据：C6、Extensions 的 Skill／AgentConfig 精确记录，以及 Brain 第 5 节的渐进发现。

实现准确版本、正文、来源、前提、反例、依赖、冲突和退出规则的 Skill 登记及有界加载；AgentConfig 固定 Brain、能力和控制上限。当前权限先于读取，普通内容不能成为受信系统指令，配置不能扩大 Grant；注册、加载、撤回、重开和 Task Snapshot 的实际使用均须验证。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03 catalog宿主首片：准确Knowledge Content纯Tx门禁与普通材料用途接入Development；公开Skill注册→实际原validation Job→加载准确body/usage可运行。固定Gov公开tracer通过Go overlay在当前App真实执行：旧版RED5.873s current_knowledge_gate_unavailable，门禁接线后真实SQLite/PG GREEN14.426s。未启用Task知识选择、未改变默认File提案、未授Grant；Config/Context/Brain当前选择及物理控制边界仍待后续装配。旧publication nil policy只沿准确原reserve恢复，不随新增用途换policy。

2026-10-03 有界领域与真实消费已接入：Skill/AgentConfig 准确 Content、闭合合同、原 validation Job、永久版本绑定和撤回/重开；Config.Knowledge 的普通 Packet、完整来源、固定 Brain、有效 Capability/Binding 和数据锁进入实际 Task Snapshot。原 Task/预算/Decision 同 Tx 建 holder，Brain 出站及返回、缓存提案和行动准入核当前 selection。输入/原模型 cost bound 超限时 0 POST；输出 reserve 收紧至 128；两行动超过一行动上限时没有工具 Use；未选 Write 在原闭合模型合同处拒绝；实际 File 读回保持可执行 leaf lock，最紧 30 秒由 Task 冻结。Agent 能力集合仍唯一，同一能力的两候选绑定按原索引保留，未增加角色或 Grant。准确接口及范围见 internal/governance/KNOWLEDGE.md。

2026-10-03 验证事实：纯域原 holder/来源/CAS SQLite race 26.972s、PostgreSQL race 29.741s；公开配置正例、输入、费用、行动数、能力、真实读回/时长、两绑定及撤回均有两库 normal 证据。准备字节恢复的正式修复 d67e7a9 后，仅受影响 GUI、旧 leaf 和知识读回两库 normal PASS226.585s。知识 focused race 原745.229s 为失败：前八子例通过，撤回两子例最后重开查询超出90秒测试观察 context；保留完整日志，不记成功。仅撤回 observer 改为既有三分钟集成上限，原 Task/Grant/Content 期限不变；专项两库 race PASS174.234s。撤回保持原唯一 POST、无提案、原已知0.00024 USD费用与 cfg=nil 重开原账单。执行环境 knowledge18-verification.json、knowledge18-focused-race.log、knowledge18-withdraw-race.log 和 knowledge18-prepared-byte-regression.log 绑定证据；旧扩包20分钟 timeout及GUI失败也保留，不由后续绿覆盖。

本轮已实现 R4 规定的准确小目录直接装载（Skill≤8、候选≤32、正文64KiB、普通Packet128KiB）及本地同库消费；大目录渐进检索只列为未开放优化，不作为本票未完成理由。当前 partial 待工单17的内部/外部委派实际消费原父 selection 的 Agent 能力/控制与父 Grant 交集，以及最终双轴审查。准确纯Tx端口已交该负责方；未经实际委派验证不以控制元数据宣称接线完成。跨 owner 知识 holder 交接与生产规模/质量另列资格。普通知识与数据锁不提供系统信任、程序 readiness、授权或 Task 成功裁决。

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。
