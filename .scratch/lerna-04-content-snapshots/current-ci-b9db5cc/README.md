# 准确 b9db5cc 的 CI 资格

已推送的 `b9db5cc67647a15d6556fe92941757034a1e80d1` 对应 [run 37295630473](https://github.com/ruipengliu/lerna/actions/runs/37295630473)，实际 completed／success。contracts job `111716037181` 的11步骤与 durable-admission job `111716037468` 的12步骤全部成功；[observed.json](observed.json) 保存准确 head、job 和步骤。

[contracts 原脱敏日志](contracts-sanitized.log)为44257字节／537行，SHA256 `b9eddc1d389cce60d97b4f016d329102360e6766e8b5f2953b2cce57471662ac`；37项执行工具、44项TS及合同正反序、构建检查通过。[durable 原脱敏日志](durable-sanitized.log)为53203字节／593行，SHA256 `3ae5334cab1ac1fdbb55963eeebdb96f235c70c618f123f12c5fdc6849f14cf3`；正常／竞态 Recovery、Component 与 fixtures 均通过。保存的是连接器返回的完整脱敏字符串，字节精确；root 完整阅读结构化23步骤与有限包／入口结果，没有声称逐行人工阅读全部1128行环境准备日志。

此 head 包含 Run 取消原因保留修复，Component 仍为原 Content 49／48 两组。未包含03政策时钟／Input新优化、06新增恢复测试或三 Content／两 Durable 分组候选，不能用该 CI 替代候选资格。原失败及每项首次 native Close UNKNOWN 各自保留。
