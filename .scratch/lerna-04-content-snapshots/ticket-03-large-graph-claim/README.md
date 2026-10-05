# 大图 claim 失败复核

采用[完整决定](decision.md)：保持原限额，在明确独占执行槽内作一次带有限阶段诊断的完整 B race 复验。实际 ErrClaim 不能定位失效阶段，不盲重试或扩大原 scope 的预算；按实际第一错误决定最小修复。原失败和执行重叠范围保留。

原报告逐字节归档，校验和与来源见 [provenance.json](provenance.json)。此为静态决定，不是 race 通过、正式票据退出或整片验收。

## 原界独占诊断的实际结果

[完整结果](diagnostic/results.md)、[原日志](diagnostic/original-race.log)、[五文件 overlay manifest](diagnostic/overlay-manifest.json) 与 [逐文件原字节来源](diagnostic/provenance.json)归档了真实失败：原 5 秒 Claim 在计算材料读取期间到期，尚未 Prepared 或 worker 发布。调用方原 30 秒尚有余量；嵌套计时不相加，数据库、解码及物理读取的子成本仍未知。该证据没有产品优化或期限放宽结论，也不重写旧共享负载失败的原因。
