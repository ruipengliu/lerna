# e0c66f6 的实际 CI

[运行 37305738280](https://github.com/ruipengliu/lerna/actions/runs/37305738280)在准确提交 `e0c66f6dec1325f648060248207056de1b0ced06` 成功。contracts 与 durable-admission 两个 job 的全部 23 个实际步骤均成功，包括 `make check`、真实恢复客户端生命周期、普通与竞态集成。逐步骤结果见 [observed.json](observed.json)，已脱敏原日志随本目录保存。

此提交的产品源码仍为已接受 b9 源码。CI 使用原 Content 49／48 两组及 Durable 60 一组；不包含 d0 分组候选、03 Input memo 或 06 工作树改动，不将本次成功转作这些候选的资格。旧 461 CI 的实际失败仍保留。
