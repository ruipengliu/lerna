# 切片 01 远端 CI 核验

状态：已确认旧检查点失败，修复与新提交复验待完成。

本地 gh 访问 Actions API 返回 Forbidden。随后通过已连接的 GitHub 工具读取 push workflow collection，确认这不等于远端 CI 不可核实。combined status 无条目及仅筛选 pull_request 的 wrapper 空列表均不能作为 push CI 成功或未运行的依据。

| 提交 | Run | 结果 |
| --- | --- | --- |
| `48742260f702ac31affc5fcdbe1aff0374ffa669` | [37136281812](https://github.com/ruipengliu/lerna/actions/runs/37136281812) | failure |
| `70cbf660104fc69109bbc7e7129080e696b0bf27` | [37137227315](https://github.com/ruipengliu/lerna/actions/runs/37137227315) | failure |

准确失败日志来自第二次 run 的 contracts job `111244107202`。checkout、准确 Go 1.27.1、Node 24.19.0、pnpm 12.8.1 和 make bootstrap 均成功；make check 首步 `node scripts/check-go-format.mjs` 报 `spawnSync rg ENOENT`，所以未执行后续验证，不得将其描述为远端测试通过。

根因是构建脚本使用未声明的 ripgrep 依赖。最小修复取舍见[CI 决定](ci-decision.md)：使用必要 Git 的准确文件清单，不安装额外工具或绕过检查。

新提交的真实 workflow URL、head SHA、步骤和最终结论待推送后追加；旧失败不改写为成功。
