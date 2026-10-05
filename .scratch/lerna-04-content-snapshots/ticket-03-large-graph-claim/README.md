# 大图 claim 失败复核

采用[完整决定](decision.md)：保持原限额，在明确独占执行槽内作一次带有限阶段诊断的完整 B race 复验。实际 ErrClaim 不能定位失效阶段，不盲重试或扩大原 scope 的预算；按实际第一错误决定最小修复。原失败和执行重叠范围保留。

原报告逐字节归档，校验和与来源见 [provenance.json](provenance.json)。此为静态决定，不是 race 通过、正式票据退出或整片验收。
