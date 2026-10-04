# 12 simulated-gui

Status: resolved
Implementer: execution_impl

范围是三台独立持久模拟手机的完整点击、滑动、输入、返回及原观察/Attempt/epoch恢复，不声明真机。

## 完成依据

选定GUI capability使用原有限观察→动作→再观察/独立磁盘真值，保留旧set_note/open_notes/press_home/set_wifi记录。driver`5818d056`的真实SQLite/PG公开Execution矩阵含三台设备18手势、伪造旧观察/当前许可/StartBarrier、中断/unknown/实际介质缺失、原库重开不重发、取消/本人接管/并发fence；每轴准确refs/事实存于`/workspace/harness-dev-environment/gui-verification.json`，全部实际exit0。

后继公开Task→Gov→Execution装配`9ba68da`两库真实GUI race290.484s、原Operation冻结leaflock后改配置重开138.953s、GUI+默认File报告151.697s，见`action-assembly-verification.json`。显式Grant/capability/binding/resource、Context不消费once、原Snapshot之后撤权阻效果与跨binding拒绝有证据。原prepared编码耐久后继fix`d67e7a9`与独立Knowledge/GUI业务回归226.585s另记录，旧失败不改通过。

闭合方法、准确refs与公开范围见 adapters/execution/GUI.md。SQLite/PG持久模拟器不是物理Android/iOS、任意App、所有端云部署或生产容量；这些资格独立验收。
