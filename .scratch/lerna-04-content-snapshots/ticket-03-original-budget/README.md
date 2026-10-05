# 原Snapshot耐久编译预算决定

采用[原文](decision.md)，准确字节、来源和固定版本见[provenance](provenance.json)。原完整SnapshotRef固定最多三轮、累计材料与四份派生产物验证回读预算及绝对期限；输入revision、重开、失败和unknown不重置。显式预算reader只传编译路径，worker保持原Prepared／Publisher职责。追加fixture0004，不改已运行0003或原Content迁移。

原三轮真实对照后第四轮仍成功的首业务red为1.974s，native1／groupAbsent；root已读取源码和原日志。报告是采用的内部接法，尚无预算green、累计字节资格或票据接受。普通三轮正常对照不得自动扩大原65536读取额度。
