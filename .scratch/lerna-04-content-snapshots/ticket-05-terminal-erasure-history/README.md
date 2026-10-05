# 原责任擦除结论与历史保持

采用[决定原文](decision.md)，准确字节和来源见[provenance](provenance.json)。同原准确责任已有正向全holder ACK时，普通自然phase合并保留erased和完整历史，动作继续union、原期限不刷新；pending规则与其他change独立。普通入口不能制造新erased，不新增反向锁、迁移或通用transition flag。

真实2.309s首失败已证明同原责任在自然到期后倒退pending并丢旧holder历史；root读过实际源码和日志。最小修复及正常／race仍待验证，不代表该票七项AC完成。
