# 21 independent-system-implementations

Status: claimed
Blocked by: 01, 02, 23
Implementer: web_impl

依据：A2 与扩展合同要求 Brain、Memory、Executor 各有第二种独立实现，至少一个异构语言组件走真实 WSS／gRPC。

固定实际已开放的有界系统端口与同版合同，为三个系统提供行为独立的第二组件；不能仅替换引擎、数据库或进程名。用原正常、丢回执、撤权、恢复及当前权限轨迹验证可替换性，至少一个组件为异构语言并走真实传输。记录准确开放 profile、仍关闭的方法与独立实现证据，不扩大声明为完整可选 profile。

## 完成依据

三种系统的独立组件与互操作均有运行证据后记录准确提交和制品。不得把设计目标或同一 Service 的不同配置当作验收通过。

## Comments

2026-10-03：固定源码复核 a5410e4 确认现有引擎／Store 替换尚不能证明系统第二实现，补入原实施任务图。
