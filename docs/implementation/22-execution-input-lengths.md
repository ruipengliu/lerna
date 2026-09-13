# 22 票执行输入的有界长度复用

基点 `5acf4e7`，本增量只减少执行输入的重复元数据观察，不代表研究流程完整计量或预算验收通过。

Content READ 要求已知精确范围，不能用固定大 Limit 替代 GET。executioncontent 只在成功完整读取后记录最多八个引用的长度；再次读取按原16384字节分块实际 READ。每一块仍经 Content 当前授权与完整存储摘要校验，并核对返回引用、大小、资源、用途、媒体类型及可用性。未保存正文、来源权限或授权决定。零长度输入不缓存，避免零次READ路径跳过当前验证。

WithContent 仅替换受控消费端口并共享长度事实；fetchoutput 的请求视图因此可以保留长度，而每次观察继续绑定原 Request 和 ActionBinding 的预算。新任务、重开存储和恢复均不从长度事实获得新的权限或额度。

真实 SDK 计量反例最初仍为六次；修正后 Invoke GET/READ、Run READ、结果保存两个GET，共五次查询。真实 Content 测试以两万字节JSON验证两次读取共五次调用（三次+两次），三个并发读取各自执行两个READ，discover-only撤权后读取拒绝且不返回正文。额外核对能力资源变化与过期均拒绝。SDK WAITING 存储重开仍为六次，因为重建适配器没有沿用旧进程的长度表。

定向相关race通过（fetchcheck10.841s），完整 executioncheck/asynccheck race通过（19.992s/14.657s）。增强资源及过期断言后的SDK/输入定向race通过（4.436s），vet/diff通过。以5acf4e7为基点，仅审查本增量指定文件的独立Standards与Spec两轴均无actionable findings。研究装配的双页面流程此前耗尽64次仍未解决；最终全仓验证未执行。
