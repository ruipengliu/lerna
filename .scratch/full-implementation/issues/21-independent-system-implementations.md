# 21 independent-system-implementations

Status: resolved
Blocked by: 01, 02, 23
Implementer: web_impl, native_source_impl

依据：A2 与扩展合同要求 Brain、Memory、Executor 各有第二种独立实现，至少一个异构语言组件走真实 WSS／gRPC。

固定实际已开放的有界系统端口与同版合同，为三个系统提供行为独立的第二组件；不能仅替换引擎、数据库或进程名。用原正常、丢回执、撤权、恢复及当前权限轨迹验证可替换性，至少一个组件为异构语言并走真实传输。记录准确开放 profile、仍关闭的方法与独立实现证据，不扩大声明为完整可选 profile。

## 完成依据

三种系统的独立组件与互操作均有运行证据后记录准确提交和制品。不得把设计目标或同一 Service 的不同配置当作验收通过。

## Comments

2026-10-03：固定源码复核 a5410e4 确认现有引擎／Store 替换尚不能证明系统第二实现，补入原实施任务图。

2026-10-03 完成：独立 Node24/原生 SQLite 的 Brain、Content/Memory、Executor 均为真实第二系统实现，不导入 Go 领域 Service、不共用权威数据库。准确有限方法族、关闭的任意模型/fullact/report/远程写等能力见 `adapters/alternate/README.md`。源码741dc21基础19类26项 race118.546s；源码4840ccc基础+外来Memory28类40项完整race343.142s，均实际退出0并保留原版本/完整日志与失败。

默认 development.App 的显式 ForeignConsumers 与 ForeignSourceTLS、原Source四方法和准确current/Policy/CopyHolder对接已开放：Source管理员批准实际Native inspect-owner ConsumerScope/database、独立入站peer/holder准确generation/roles、purpose/location；默认nil关闭，不借用Source用户完整token。管理pair永久冻结原库/原tuple；oldRef/Policy不改写；Native配置显式expected_database_id。HTTPS复用20准确GatewayTLS引用、冲突拒绝，不改application内部mTLS。

准确执行源码7a050797946bec9f5aad1f18737eb6ede5dda21a，开始结束clean/stable，两库Source→独立Native Memory实际App.Run HTTPS/WSS affectedrace墙钟245.487s actual exit0：配置70.685s；Native两库236.305s；115000原字节三用途共6片、真实注册丢答复、原receipt恢复、Native SIGKILL、原Scope/ContentRef重开、公开Source close、新业务拒绝、原release/residual、正文6→6。完整元数据与日志为 `/workspace/harness-dev-environment/configured-native-source-app-run-tls-verification.json` / `configured-native-source-app-run-tls-race.log`。前一Gateway TLS fixture源码b261实际252.111s通过独立保存，未改成App.Run证据。

实际配置与运行说明、历史profile证据及边界见 `adapters/development/NATIVE_SOURCE.md` 和 `adapters/alternate/VALIDATION.md`。本工单关闭的是上述准确独立系统端口与互操作profile，不声明默认Orchestrator整个report链已替换为Native，也不代表真实供应商质量、生产身份、真机或跨AZ资格。16/17的设备/Agent角色组合由各自实际验收分别记录。
