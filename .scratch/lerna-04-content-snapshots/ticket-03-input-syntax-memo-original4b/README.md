# 03 Input 语法复用：原四容量 B

当前 332 文件候选树新构建普通／竞态两个独立二进制，按原四项容量命令各执行一次。普通全部通过；竞态两个大图正例失败，原 65、200000／262144 及 duplicate 对照通过。原 caller30、Claim5、package／outer120 保持。

普通 64 祖先与 62 材料均实际 Prepared／Completed COMMIT、两次 Publish／ReadPublished 和完整原公开尾。竞态 64 祖先 compile/bind 到 29.867749s，原 caller 仅余约 132ms，在独立锁观察处截止；尚未 dispatch。竞态 62 材料已有 Prepared COMMIT，第一个 Publish 在 Access.Current 截止，尚未调用 worker Content.Put；最终 Claim 晚 31.326ms，无 Completed／ReadPublished。该轮不是新的 worker Put 效果 UNKNOWN，历史真实 Put UNKNOWN 继续保留。

完整 288 行日志见 `raw/`，原结果见[普通](results/original4b-normal-results.md)和[竞态](results/original4b-race-results.md)。[索引](archive-proposal-index.json)区分逐字节原件、原账本准确区间与有限 outcome 摘录。332 文件清单只保存一次；四份完整原 outcome 保留准确引用及哈希。Root 已独立核对 scalar／argv／native 身份、raw、区间及当前二进制／源码，见[核验](root-independent-archive-audit.json)。原 Go 输出 FD Close UNKNOWN 与 own Read FD 首次 Close ACK 分开保存；未执行 catalog 审计。

[下一决定](adopted-next-decision.md)只准备当前 Compile 的固定阶段计时，区分纯计算、材料读取及真实发布。测量尚未执行，未采用另一项产品优化。普通成功不抵销竞态失败，也不证明净收益或 PG 根因；票 03 尚未接受。
