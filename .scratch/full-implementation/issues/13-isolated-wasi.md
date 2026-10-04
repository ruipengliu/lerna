# 13 isolated-wasi

Status: resolved
Blocked by: 01, 02, 04
Implementer: governance_impl

依据：Execution 第 7 节及 F15；不重放已开始 cell，只提交成功的完整被动命名空间。

锁定并探针验证 Linux WASI 解释器、独立隔离进程与资源限制；关闭文件、网络、凭据及子进程入口。原 barrier 和持久发送标记先于进程启动；取消等待真实退出，原未知记录不重放，原命名空间 CAS 和来源共同提交。

## 完成依据

实现、公开行为、真实负责方和必要故障正反例通过后记录准确提交与制品。外部资格单列；尚未完成的本地路径不得归因于缺凭据。未达成的目标保持 partial。

## Comments

2026-10-03：全项目范围复核后补入原实施任务图，未改变用户授权或领域裁决。

2026-10-03：选定 Linux amd64 受限 Preview1 profile 已有实际代码与隔离/崩溃验收，说明见 adapters/wasi/README.md；原 namespace、Operation 与 Job 同 Tx CAS，完整 Source、原 attempt journal、实际退出与原账单不重放。SQLite 公开功能 22.789s、PG 22.695s；新增原 barrier CommitUnknown 零入口 RED 2.218s → GREEN 2.164s；最终含该 case 的 SQLite/环境回归 35.375s，SQLite race 74.828s、PG race 82.560s。实际宿主 SIGKILL/内核 PID 观察、恶意 CPU/wall/内存/输出/宿主访问、旧 generation、丢答复删原程序并重开均已运行。crash test 的 runner 观测等待有界 25s，不刷新原业务期限。CI 已定义安装系统依赖并实际运行 probe；未宣称 hosted CI 已运行。

具体前提：Linux amd64、静态准确 worker/bwrap/prlimit/hash、user namespace 与硬限制 probe 均须实际通过；worker 独占原私有 root。默认 nil 配置不开放，非worker只登记原 manifest 合同，真实 Task/模型行动接线归工单15。其他平台/原生程序、自定义 guest hostcall、跨设备/跨 owner 权威、生产 AZ/断电/规模资格仍未由本片验证，不以该 profile 宣称整个工程或完整 Execution profile 已完成。

两轴审查固定 6c2d3df…f594b82：确认的 journal 写入未知后容量漏计与关闭环境清理 Job 错误吞没，统一由 Task implementer 在 8dd41ca 修复并已吸收。实际 native rename 后 EIO 保留最后额度与原 Attempt/重开；实际 SQLite INSERT trigger 故障使环境、原拒绝回执和清理 Job 同事务回滚。该两项 SQLite race 17.124s，PG 原 quota/CPU 丢回复重开 race 20.439s；SQL trigger 故障仅在 SQLite 验证。此 resolved 状态只指上述选定受限 profile，不扩大为未验证的平台、外部权威或生产资格。

后继实际Task/Worker PG完整Cell与File报告actual287.388s/原5min deadline前完成、独立条件与Result出版/CPU及USD结清、真实Worker join和原库重开见工单15。CPU minimum-invoice旧新guards组合整体FAIL独立保留；后继focused247.741s两库racePASS和Code withdrawal两top181.32/206.85s按 /workspace/harness-dev-environment/wasi-cpu-minimum-invoice-verification/cpu-final-verification-focused-complete-20261003T225400Z.json准确pins分别取证，不由Environment driver资格推所有后继组合通过。
