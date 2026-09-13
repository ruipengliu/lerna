# 11 票真实旧编码样本

由实际 `d59cf762b7b7f3ca7b6a0b24d64fc1738500a575` 源码构建 contractcheck，分别执行 `async-crash-probe <临时目录> cancel-registered` 和 `report-saved`，子进程均退出 73。authority.gob 直接提取旧 SQLite 的 document，未经过新版本编码器；target.db 使用 SQLite backup，checkpoint.json 保留合成测试的原身份。

两个快照分别保留取消已登记但结果未知、完成证据及未投递 Outbox。升级测试同时写入第二准确描述符分区，再检查原调用、许可、预算、取消、目标效果和报告。同步旧编码另由既有 testdata/10 回归验证。

仅含离线合成测试的随机登录值和授权校验材料，不含生产凭证、供应商密钥或签名私钥，不能用于真实服务。
