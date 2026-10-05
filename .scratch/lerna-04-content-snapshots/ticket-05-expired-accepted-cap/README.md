# 原接受保留期限到期的清理因果

采用[决定原文](decision.md)，准确字节见[来源](provenance.json)。严格核对原准确版本、完整保存主体／用途、持久期限及结构闭包；原cap到期后，当前宽策略不能复活它。复用原seal／holder／Job及全ACK，原清理执行期限不刷新，整页CAS和最后时钟保持。

真实2.216s首失败已证明旧读取expired、原字节仍在且新version正常时，消费者没有建立seal。仅target自身cap已验证；修复后真实删除／ACK、祖先与分页等范围仍待独立验证，票05继续claimed。
