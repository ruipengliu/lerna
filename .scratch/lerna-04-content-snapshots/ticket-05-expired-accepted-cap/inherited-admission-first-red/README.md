# 祖先原接受上限与准确 AdmissionTarget 的首条业务失败

[原日志](original.log)与[原字节来源](provenance.json)记录普通准确测试2.223s失败：当前消费入口拒绝原AdmissionTarget key，尚未进入seal／擦除。原caller20s、Go30s、wrapper120s、2秒接受上限和原cap＋minute责任期限保持。

已执行前置：源A真实接受2秒cap、当前政策Rev2续宽不抬原cap；源B仍live；派生target有两个真实来源，实际accepted回执的cap精确等于A；V2正常。公开有限页取得准确原target/source/fullSubjectPurpose/key／due／deadline／watermark，真实重开后等原cap＋20ms，原责任pending／accepted_retention_expired且执行deadline仍live。A和target公开读取expired，三份独立原字节仍在，B／V2正常。

首Consume返回scope错误。红后target seal、全holder擦除ACK、再次重开、祖先字节保持与回执重放／command出版历史尾段均未执行；只编码了正常阶段原回执。产品未修复，窄因果决定待审，不能宣称green或本票完成。
