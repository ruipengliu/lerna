# 当前读取目标封闭门禁

03 实际检查点 `59fe653` 正常合并已交付 05 的准确 `dadd801`，得到 `b28b2e3`。root 完整机器核验实际 parents、329 源绑定及原差量文件 SHA；人工阅读相关差量，未声称全文阅读大型 JSON 或整树 diff。旧 SQL 测量草稿明确失效，须在当前语义资格完成后重新绑定。

[已采用决定](adopted-body-seal-decision.md) 要求在 processing 的共享观察回调拒绝目标 `BodySeal`。原源码 `4a4440c` 在真实 Seal 后仍读取正文；已合入的祖先封闭门不能覆盖零来源目标。

修正前一次真实反例已执行：普通包 1.195s、实际 exit1、PID/PGID3635393/start15248641、Wait 和当前原组 absence 确认。[完整原日志](context-target-body-seal-first-red-normal.log) 的两独立场景都先通过真实普通读取、Seal、独立 alpha、原两 holder/期限/绑定、metadata 下 Forbidden 而非 Gone、固定回执/发布历史及重开尾，最后才失败：原读取及重开读取返回 nil error 和非零正文。

[root 独立审计](root-independent-red-audit.json) 完整机器核对 330 源与 b28 blob/格式后新测试、prelaunch 和实际 outcome；人工全文读 16 行原日志。该反例不是装配或机械错误。隔离测试 scope 的正常 Cleanup 不等于业务 erasure/all-holder ACK，历史 UNKNOWN 不补证。

[候选修正](target-body-seal-fix/target-gate-only.diff) 仅增加三行目标封闭拒绝，精确逆变换还原 4a44；原身份、当前权限/时钟、I/O/hash 和已合入祖先门保持。[候选计划](target-body-seal-fix/commands-static.json) 的格式、普通与竞态资格在该静态文件记录时尚未执行；后续实际结果另附，不以静态文件冒通过。票 03 及原大图竞态容量仍未接受，完整 1.2 继续关闭。

[归档映射](archive-map.json) 保存每份原路径、字节与 SHA。历史 JSON 中的 outside 路径保持实际当时事实。

后续实际资格：目标修正的纯逻辑普通 0.044s、公开普通 2.681s、公开竞态 4.625s 均通过；原正常读取、隐藏祖先撤权对照及责任／正文／回执／重开尾保持。真实隐藏祖先在对象读取期间封闭的独立对照，普通 0.602s、竞态 2.893s 通过。两者均在返回前得到可判断的处理拒绝和空结果，不把 Seal 当成 Gone。[root 独立机器核验](root-independent-green-audit.json) 核对全部 330／331 源、七阶段 pre/post、原日志哈希、Wait 和当前组消失；大型 JSON 只完整机器核验，未声称人工全文读。旧容量失败仍有效，026 成本测量须重新绑定当前源后单独执行。
