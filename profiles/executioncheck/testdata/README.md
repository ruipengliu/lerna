# 09 持久编码样本

由实际提交 `19a323b69ee1205468c668777e718d661cc96e1a` 的编码器生成：构建该提交的 contractcheck，运行 `updates-crash-probe <临时目录> input-committed`，预期子进程退出 73。以同一版本的 sqliteauth.Store.Load 导出 State.RuntimeData；checkpoint 只提取 Ref。

样本只含公开测试目标、输入引用、主体和原操作/回执；不包含授权 State、凭证、签名私钥或内容正文文件。新版本验证旧输入事实及新增执行字段默认值，并在同一 authority 中运行新的完整执行后核对旧 Runtime 记录未被覆盖。旧引用正文可读性不由本样本证明；已有 01–09 回归继续承担原有内容/恢复行为验证。
