# 03 Input 解码复用：真实存储语义资格

当前 332 文件候选树上，12 项真实 PG 测试正常／竞态各 12 top、18 sub 全部通过。测试分别验证同一 dispatcher 热输入修订、原预算与锁等待期限、当前权限及控制、目标／祖先正文封闭、读取后的撤权，以及真实祖先 policy 锁超时原因链。正常包 20.592s，竞态包 36.109s；原期限不变。

完整 183 行原日志见 `raw/`，有限结果见[原结果](semantic-results.md)。[索引](archive-index.json)记录 16 份逐字节原件及其 SHA256；[当前树](formatted-source-manifest.json)只保存一次 332 文件清单。原大 outcome 的准确哈希和三个 native 的完整 argv、原登记区间、实际 Wait／当前组缺失、原日志首次 Close 见[root 有限摘录](root-compact-actual-provenance.json)。完整 8551 字节格式差量已检查，两个既有测试提案没有语义变化。

该资格证明复用成功解码的纯输入表示时，原 SQL 读取、修订、权限、预算和拒绝语义保持。它不证明容量竞态、整体票退出或当前 CI。历史原 Go 输出 FD、CPU profile FD 的 Close UNKNOWN 与首次 Put 效果 UNKNOWN 保持；未执行 PG catalog 审计。
