# Input 新私有API：诚实编译 RED

proposal格式化实际0，由10385B/2483102c变为10571B/abd49508，完整before/diff保留。当前331产品字节前后完全相等，Binding helper仍fe6e723b；没有Input memo实现或baseline shim。

一次指定normal实际1，完整13行/1571B raw中9条编译诊断全部为 `undefined: inputDecodeMemo`，包 `[build failed]`。没有测试 `=== RUN`，六个top测试函数的全部测试体均未执行。README的七项是语义控制类别，不是七个top测试。该失败只能记为新内部API缺类型的 compile RED，不能声称 parse-count、严格错误、scope或clone断言已产生行为RED。

| 阶段 | PID/PGID | start tick | actual exit | Wait/current group |
| --- | --- | --- | --- | --- |
| proposal fmt | 4014613 | 16839315 | 0 | ACK / empty |
| compile | 4014619 | 16839328 | 1 | ACK / empty |

normal原raw `/workspace/lerna-content-03-137311247276/context-input-proposal-compile-red-normal.log`，SHA ef7c18e73c627607fe6760cf8f620dadedb0f5db87d92eb78e1b1b36544cd2dd；fmt原raw1行/39B/06fd0fb3。所有14行raw与完整fmt差异已阅读。两个原PGID当前empty、actualWait ACK；两个own raw writer各fsync/firstClose ACK。原scope只登记process_group/nativeexit，没有运行测试body、PG/object fixture。

已STOP / RELEASE LOCAL，不自动实现memo/重试或额外native。此前真实Binding指针行为RED及其已通过normal/race、原大图业务RED与所有FD/effect/observer UNKNOWN均独立保留。
