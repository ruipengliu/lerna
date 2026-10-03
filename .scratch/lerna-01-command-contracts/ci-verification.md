# 切片 01 远端 CI 核验

状态：本地 portability 修复与新准确实现提交的远端 CI 均已通过；旧失败保留如下。

本地 gh 访问 Actions API 返回 Forbidden。随后通过已连接的 GitHub 工具读取 push workflow collection，确认这不等于远端 CI 不可核实。combined status 无条目及仅筛选 pull_request 的 wrapper 空列表均不能作为 push CI 成功或未运行的依据。

| 提交 | Run | 结果 |
| --- | --- | --- |
| `48742260f702ac31affc5fcdbe1aff0374ffa669` | [37136281812](https://github.com/ruipengliu/lerna/actions/runs/37136281812) | failure |
| `70cbf660104fc69109bbc7e7129080e696b0bf27` | [37137227315](https://github.com/ruipengliu/lerna/actions/runs/37137227315) | failure |

准确失败日志来自第二次 run 的 contracts job `111244107202`。checkout、准确 Go 1.27.1、Node 24.19.0、pnpm 12.8.1 和 make bootstrap 均成功；make check 首步 `node scripts/check-go-format.mjs` 报 `spawnSync rg ENOENT`，所以未执行后续验证，不得将其描述为远端测试通过。

根因是构建脚本使用未声明的 ripgrep 依赖。最小修复取舍见[CI 决定](ci-decision.md)：使用必要 Git 的准确文件清单，不安装额外工具或绕过检查。

新提交的真实 workflow URL、head SHA、步骤和最终结论待推送后追加；旧失败不改写为成功。

## 本地 portability 修复与证据

2026-10-03，独立 `codex/contract-ci-portability` 基于集成 `bab6919`。`scripts/check-go-format.mjs` 使用 `git ls-files -z --cached --others --exclude-standard -- '*.go'` 替换未声明的 rg。NUL 分割仅移除尾部空项，不 trim 文件列表或拆换行；保留参数数组，路径加 `./`，只跳过 lstat 的 ENOENT，其余发现 / 工具错误仍失败。空 Go 列表不执行无参数 gofmt，Git 与 gofmt 调用各有 10 秒有限截止。没有新增依赖、永久镜像测试或产品合同变更。

在只含 node / git / gofmt、明确无 rg 的临时 PATH 中先复现旧脚本 `spawnSync rg ENOENT` red；新脚本对格式正确的同一 checkout 正常通过。14 个实际临时 probes 全部按独立预期通过：

- 无 rg 的已格式 checkout 接受；未跟踪、未格式的 Go 文件拒绝，规范格式后的同一路径接受。
- 含空格、真实换行、开头连字符的三个文件名均完整作为单一参数处理，分别得到坏格式拒绝与格式正确的正常控制。
- ignored 的未跟踪 Go 文件由 Git 排除；暂时删除的 tracked Go 文件只在格式发现阶段跳过，随后恢复。
- broken Go symlink 仍通过 lstat 发现并因 formatter 读不到目标而失败，未把这类读取错误吞成“文件已删除”。
- 全新空 Git checkout 的 PATH 没有 gofmt 也能有限结束，证明空列表不调用 stdin mode；非空 checkout 缺 gofmt、缺 Git 和普通非 checkout 中 Git 命令失败均明确拒绝。

所有 probe 文件、symlink、临时工具目录均清理，tracked 文件原样恢复。锁定 make bootstrap、受影响 make lint、完整 make check 与 git diff --check 均通过。完整检查保留 16 lifecycle / 34 TS tests、全部 Go tests、13 generator 拒绝 / 8 Schema 变更 goldens、生成零差异、158 共同合同前向与反序真实双向往返、摘要 / 受信读取 / 协商独立 suites 和双语言构建。环境使用 Go 1.27.1、Node 24.19.0、pnpm 12.8.1 / TS 7.0.2。此次只改文件发现，不改变并发路径，未为它重复无关 race suite。

这证明本地缺 rg 场景已修复，**不表示远端新提交 CI 已绿色**。新 workflow URL、head SHA 和真实最终结果仍由主任务在推送后追加；上表旧失败记录保留。

## 新提交远端真实结果

2026-10-03，通过已连接 GitHub 工具读取准确 push run 与 jobs / job logs：

| 代码提交 | Run / Job | 结果 |
| --- | --- | --- |
| `23bac17ba0909c7a4d49d846eb08bc63391b99f0` | [37141974245](https://github.com/ruipengliu/lerna/actions/runs/37141974245) / contracts `111258125289` | completed / success |

Run 从 17:50:17Z 到 17:51:03Z。所有步骤 success，包括 checkout、准确 Go / Node、pnpm 安装、make bootstrap、make check 和收尾。实际日志确认格式检查不再依赖 rg；生成一致性、16 lifecycle tests、34 TS tests、Go suites、158 项共同夹具的前向 / 反序真实双向往返及双语言构建都执行成功。这里使用的是 push workflow 的准确 head_sha，不是 pull_request 过滤器的空结果，也不是只有 combined status 或 push 成功。

旧两次 failure 保留原结果。当前通过只证明切片01的合同与验证设施，没有数据库、网络认证或生产故障域证据。之后的退出文档提交不改变上述受测实现代码。
