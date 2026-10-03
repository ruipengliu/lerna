# 模拟手机 GUI 合同

依据 [V2 专项](../../docs/harness-project-goals.md#v2手机-gui-操作)和
[GUI 与本人接管](../../docs/architecture/execution/README.md#6-gui-与本人接管)。
此驱动只提供多个独立、耐久、有状态的模拟目标，不声明 Android/iOS 真机支持。

`simulated_phone.action` v1 保留原四种动作及准确输入、输出和摘要，未结原 Attempt
仍由原驱动查询。v2 使用独立 `PhoneGUIDriver` 与闭合手势参数；两个版本共享同一目标
实例、资源代次和原目标日志。不得把原 v1 Attempt 转成 v2 新动作。

每次 v2 动作必须包含 resource_id、instance_id、control_epoch、observation_id、
target_version、action_before。一次动作只允许下列一种形状；其它字段拒绝：

| action | 特有必填字段 | 模拟行为 |
| --- | --- | --- |
| click | point: {x,y} | 点击当前观察树中的启用控件；首页打开笔记或设置，笔记获得编辑焦点，设置切换 Wi-Fi。没有命中控件时拒绝。 |
| swipe | from: {x,y}、to: {x,y} | 笔记编辑区域内垂直滑动，改变有界滚动位置；其它界面、水平或零距离滑动拒绝。 |
| input | text | 仅向当前具有焦点的笔记编辑控件写入准确文本，最多 4096 UTF-8 字节；允许空文本清除。 |
| back | 无 | 先关闭编辑焦点，再返回原导航页面；首页没有前页时拒绝。 |

坐标单位为固定 360×640 模拟视口中的整数像素，观察包含当前控件树、边界、文本、
启用和焦点状态。允许动作的观察窗口不超过现有 30 秒。动作在同一目标入口锁内先核
原 instance/epoch、当前目标版本、窗口及控件前态，调用原 StartBarrier，随后将状态
变化及准确原 Attempt 一同写入 fsync/rename/目录同步目标日志。因此声明 `atomic`。

动作回执不替代结果验证。消费方必须重新 `resource.observe`，再核对真实目标字节。
旧观察、错误控件或当前授权拒绝不得产生目标变化。本人接管沿原资源协议推进 epoch
并核实实际停止；未知动作继续占用资源，不能因观察或 lease 到期释放。丢结果或目标
同步失败保留原 Operation/Attempt，`execution.reconcile` 独立读取目标日志，恢复只
查询原 Attempt，不重发 click/swipe/input/back。

公开验收使用原 `execution.invoke`、资源及查询方法，经真实 SQLite/PostgreSQL
Dispatcher 推进三台独立目标。真实设备通常只能声明 best_effort，不能继承本模拟
驱动的原子校验保证。生产设备、屏幕截图介质和真实触控平台需要独立适配和验收。
