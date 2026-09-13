# 08 持久编码样本

`08-runtime.gob` 来自提交 `dea600fa09590a37601bd59de4e03a7e8496e36d` 的实际 Go 编码器和 SQLite 运行状态，不由新类型模拟旧字段。构建该提交的 `cmd/contractcheck`，运行 `answer-crash-probe <临时目录> reserved-before-dispatch`（预期退出 73），再用同一提交的 `sqliteauth.Store.Load` 导出 `State.RuntimeData`。`08-ref.json` 只保留 checkpoint 的 TaskRef。

样本含公开测试目标、内容引用、单次工作和未发出生成的 700 token 预留及原输出操作身份；不包含授权 State、凭证、密钥或内容正文文件。操作身份来自临时本地 authority，不能在测试新 authority 下重放；升级检查只验证旧事实保留及新操作接纳。生成过程不调用模型或外部服务。
