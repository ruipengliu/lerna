# 准确75f0429 CI结果

[run37281363495](https://github.com/ruipengliu/lerna/actions/runs/37281363495)实际 completed/failure。contracts 全步骤成功；durable-admission 普通集成成功，竞态失败。三组拆分及 Run 修复尚未进入该 head，不据此评价其新源码。

普通模式 Recovery33.085s；动态201 runnable=1 closure/97 Content/60 durable/43 other。closure12.428s；原两组Content49项66.461s、48项105.940s；durable48.555s、other.860s；三个fixture包18.228/.076/.549s全部通过。竞态Recovery73.650s、closure25.516s、Content首组78.128s通过；第二组在120.035s总包期限超时。当时运行 TestContentTargetAuthorizationPrecedesExistenceAndMismatchObservation/existing-other-request，栈在 Service.Get→getMetadata→Core.Within 设置事务 SQL。该用例只运行0s，不能把总包超时当作单用例15秒或数据库根因证明；随后durable/other/fixtures竞态未执行。

[实际观察](observed.json)及[脱敏原日志](durable-admission.sanitized.log)保留准确版本、字节和SHA。Root全文读有限结果摘要及从panic到make失败的完整栈，不声称人工全文读服务安装/服务日志。当前三组候选已通过机械资格、独立两轴与真实Go发现，但完整共享normal/race、新准确push CI仍待。历史CI失败保持。
