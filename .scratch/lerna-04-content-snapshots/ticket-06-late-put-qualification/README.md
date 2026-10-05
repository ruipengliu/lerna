# 06：原封闭先于迟到 Put

当前 producer 的真实正常对照、原封闭故障及配对竞态全部通过。正常 Put 成功；故障在原 Claim 提交后、Put 前暂停，父进程完成原两个 holder 的封闭确认，再释放同一存活子进程。真实 `local.Store.Put` 返回 `body_sealed`，原 first Close、Wait 和 Stop 均在原 Q 之前完成；公开失败事实、Gone、固定回执、完整历史及重开尾通过。

普通正常／故障／竞态包分别为 0.692／0.573／4.825 秒。格式、正常、故障、竞态四次实际 native 均 exit 0；原日志分别为 1／10／11／16 行，共 38 行。准确来源、26 文件绑定和 ledger 166–193 原片段见 [索引](archive-proposal-index.json)与 [root 独立核验](root-independent-archive-audit.json)。其他 25 文件保持 c11fd209 原字节。

本资格没有新增 first Close UNKNOWN。此前八个真实被杀进程的 UNKNOWN 继续保留，不以组消失或恢复成功补认证。PG catalog 审计、当前共享 producer 的原五场景回归、最终检查、审查与整合交付仍待完成；票 06 未退出。

采用[最终范围决定](adopted-final-scope-decision.md)：票 05 已有真实二级副本离线及原生删除失败资格，06 不新增子进程前缀组合矩阵。此处不证明网络断连、原容量或整片 04 退出。
