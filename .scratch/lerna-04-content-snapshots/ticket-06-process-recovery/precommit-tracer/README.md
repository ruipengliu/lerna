# 真正发布事务提交前的进程恢复

06 部分资格提交 `3d60d1ef899ccbf9cd4f35fc43f5e057f04c7b70`，票仍 claimed。真实 Store.Within 执行原 Service 回调，实际 SaveVersion 保存准确 published Ref、实际 Complete 使用完整原 Claim；回调返回 nil 后、原 Core Commit 前设置有限同步点。父进程立即 release／KillWait，不在持锁短事务窗口读取业务行。生产代码、三份迁移与原 caller30／child20／Claim5／work4／Tx3／statement2／lock1／Go120 均未变。

- [普通](precommit-normal-control.log)：0.389s，release 后真实 Commit、首次双 Close ACK／Wait、重开原 published 正文、固定回执和完整独立对象尾通过。
- [真正 SIGKILL](precommit-sigkill-fault.log)：5.534s，实际 Wait／EOF／Stop 后公开观察原 preparing 和固定回执；原 Claim 自然到期，代次2从原 Job／staging恢复，第二重开发布及原完整尾通过。
- [配对竞态](precommit-paired-race.log)：9.390s，普通和故障两尾通过，无 DATA RACE、native超时；实际 Wait exit0、原组当前消失。

root 人工全文读三日志8／10／13行和fmt1行，完整机器核17当前源／snapshot／原哈希及原组消失，不冒称人工全文读大型 JSON。格式后准确 manifest SHA `52f1179b491b0e1c2dcdaee74f910b9bb88c37dd5871e700a1a79c5377fd2a39`；旧16文件不变，仅已有测试场景扩展私有同步点和有限 cause class。固定分类不输出原错误字符串、SQL、凭据或正文。

被杀代次的首次原生 Close 永久 UNKNOWN：普通故障 schema `lerna_test_e7119ccdb081fbe3f1bbf03c`／objects4013822289（dev33/ino418085），竞态故障 schema `lerna_test_16e9679776e1a666b76b47d6`／objects283551839（dev33/ino418493）。保留准确[登记行36–69](exact-ledger-lines36-69.txt)；继任 ACK、父关闭和进程消失均不补旧 Close。未作当前 PG catalog 审计、业务 erasure 或其余 AC 资格。

[归档映射](archive-map.json)保存原路径和字节哈希。正式整票接受、合入、其余恢复场景及完整1.2广告仍待。
