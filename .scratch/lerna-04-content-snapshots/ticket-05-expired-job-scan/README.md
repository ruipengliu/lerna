# 过期任务反例：原失败及修复证据

当前：原64积压完整反例在固定六源WIP下普通20.418s、竞态26.476s都通过，真实出版、later-live全holder ACK、第二重开后全部旧责任/字节/回执/历史尾部执行。准确[源、原日志与outcome](scan-fix-original-full64/README.md)已保存。下面各段为历史检查点，保留当时未执行范围；其他消费者/异常记录/受影响检查与审查仍待，不接受票05或整片04。

原固定产品1a4e1d2，两独立低积压scope在准备观察阶段分别失败15.339/15.408s；native分别3279876/3285295，真实1/absence/noTimeout。V2未接纳，live/policy尾未执行，没有调度business red。

root全文读诊断8行，并机械核全公开before/after JSON字节相同；StartedAt真实时间点相同而Go内部Location为Local/UTC，Deadline与holders相同。原源码snapshot/raw/SHA保留。root采用test-only完整公开编码等值，Marshal错误必须失败，全部原字段和责任保留；产品时钟/期限不改。新的正常控制与64积压首red尚待，不用本准备失败作产品修复依据。

## 初次正常控制通过

相同原bounds，test-only完整公开编码等值后count1低积压正常15.464s，native3287994/start13731941/session27474，actual0/absence/noTimeout。root读完整5行具名log与比较helper；prelaunch源码SHA60f817d14355d1565f744be850f9309d1ae0c2d0f7190be0e204929d0823823b原字节保存。全部新V2真实beta发布/回读，后续live原seal独立两holder erased+allACK+原key物理缺失，第二reopen旧expired责任完整编码不变/alpha正文保留/所有原固定receipt与publicationhistory保持，V2正常。

产品仍1a4e1d2，64积压首business red尚未完成；此低积压通过只确认测试基础配置与正常路径。

## 真实64积压首业务red

相同60f817原测试与1a4e1d2产品，ONE原90/15/45/4/120，native3290594/start13743233/session60810实际1/absence/noTimeout，18.131s。64原版本真实逐份出版、原共同seal15s、到期20ms、trueReopen完整旧观察/alpha字节均通过；新exactV2接纳后四次原Service.Step无error，但command stillpreparing且独立准确对象不存在。root全文读8行并采用该business red。首publication断言后live/policy与后history尾未执行，不能冒第二red。

root仅授权Astra准确3b16方案的静态最小产品修正；新64正常/竞态完整尾、第三消费者和多Subject边界/受影响suite/审阅/资源/CI尚待。原64expired责任/原deadline/holder/body/history不改。

## 尚未执行的新扫描类型资格

05测试/原red证据已保存到85a1067，产品仍1a4加静态扫描WIP。root全文采用[Astra固定Record类型资格](adopted-malformed-qualification-decision.md)，准确58f54d；完整原Go字段类型与raw整数/重复键须能保守证明，不能用jsonb归一化后的部分字段相等假称合法expired。当前仅静态补足，无Go/fmt/DB/native或新green；BodyGone原shadow一致性及Seal精确原字节资格另核，不冒jsonb语义相等是全部原校验。原60f业务测试/15/45/90/4/120边界与所有旧责任不改。

root全文采用[原Seal字节资格补充](adopted-seal-byte-qualification-addendum.md)，准确5a796c：仅固定BodySeal实际stored规范编码识别与完整值绑定，保合法Unicode/引号/反斜杠/空格/HTML及原时间精度，不重写原LockVersion或构造通用canonicalizer。Record内部结构空白/键序与stored编码资格分开。当前仅静态实施，尚无fmt/native/PG或新green；所有原权限、锁、clock、Claim与holderACK仍执行。
