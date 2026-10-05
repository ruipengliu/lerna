# 准确 4610928 的 CI 失败

[运行 37300868709](https://github.com/ruipengliu/lerna/actions/runs/37300868709) 的准确 head 为 `4610928ca3cbb1130c2197e21c4168ecc2c4b5b8`。实际结论 failure：contracts 的11步成功；durable-admission 普通集成成功、竞态集成失败，共21步成功、1步失败、1步跳过。结构化步骤见 [observed.json](observed.json)，已脱敏原日志见 [durable-sanitized.log](durable-sanitized.log)。root 核对全部23步、包结果摘要及完整失败栈，未声称全文人工阅读全部726行。

该提交仅增加文档，产品源码仍为已接受的 b9，使用原 Content49／48。普通 Recovery31.635s、closure15.708s、Content62.718／98.548s、Durable49.727s、Other.987s及三夹具均成功。竞态 Recovery69.517s、closure31.873s、Content首组76.968s成功；第二组48项触及原包120秒期限，实际包120.050s。运行项 `TestContentCommandWaitRechecksReaderAndAdmission/version-envelope` 标0秒，栈在等待与原PG读取；这是整包期限失败，不证明该用例业务截止或数据库根因。Durable、Other及夹具竞态尾未执行。

Run 修复的 Recovery 仍通过。分组修复继续归 [current-ci-partition](../../lerna-implementation/issues/current-ci-partition.md)，新三Content／两Durable候选尚未包含在此CI内；不依据旧 b9 成功或本次普通成功宣布新候选合格，不重跑旧源码补证。

原日志66084字节，SHA-256 `7f79498c1433bab6bad7fd132c35cec87fc0e94770bebeae0d1e6031f8a3877b`。已记录失败的原步骤和未执行尾保持原样。
