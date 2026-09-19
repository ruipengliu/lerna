# Adapter 目录

按主要领域职责组织具体实现：`adapters/<领域>/<实现>`。分组目录不声明 Go package，也不提供转发入口。核心 module 声明所需 interface，adapter 实现它，由启动入口选择和组装。

| 目录 | 职责 | 实现 |
| --- | --- | --- |
| `research/` | 联网问答、获取与搜索 | `execution`、`httpfetch`、`replayfetch`、`jsonsearch`、`duckduckgo`、`doubaosearch`、`context`、`lineage`、`content`、`output`、`queries`、`auth`、`taskguard`、`searchprivacy`、`sqlite` |
| `memory/` | 长期记忆与生命周期 | `auth`、`cleanup`、`local`、`recoveryproof`、`sqlite` |
| `extraction/` | 端侧记忆挖掘 | `auth`、`cleanup`、`execution`、`inputs`、`localsource`、`rules`、`sourceguard`、`sqlite` |
| `tasks/` | 任务绑定与查询计费 | `content`、`local` |
| `context/` | 任务上下文与检查点 | `memory`、`policy`、`task`、`checkpointfile`、`sqlite` |
| `execution/` | 通用执行与模拟设备 | `content`、`local`、`router`、`simapi`、`simjob`、`simresource`、`simworkflow` |
| `credentials/` | 凭证管理与使用 | `auth`、`backups`、`execution`、`http`、`filekeys`、`sqlite` |
| `authorization/` | 授权与持久化 | `josegrant`、`local`、`sqlite` |
| `content/` | 内容保存与来源策略 | `file`、`local`、`policy`、`sqlitepolicy` |
| `catalog/` | 能力目录 | `auth`、`local`、`sqlite` |
| `model/` | 模型提供方 | `ark` |
| `transport/` | 跨领域传输与节点认证 | `grpc`、`ws`、`nodetls` |
| `internal/` | adapter 之间复用的内部实现 | `sqliteopen` |

## 归属与依赖

- 归属看主要领域规则，不看技术。例如获取存储放在 `research/sqlite`，记忆存储放在 `memory/sqlite`。
- 跨领域适配按主要职责归属：`tasks/content` 负责查询计费，`context/memory` 负责记忆进入任务上下文。清理实现随失效事件的拥有者归属。
- 多个领域使用不自动成为公共 module。只有已经存在的共同规则才放入 `internal/`；同名 Journal 或同用 SQLite 不代表事务语义相同。
- 核心生产代码不导入 adapters。adapter 之间允许必要的具体组合依赖；目录分组不引入新的依赖入口。
- 调用者和测试使用同一个 interface；实际替换的 seam 与权限、预算、恢复行为保持。

## 导入路径迁移

原平铺路径已迁移，不保留兼容转发包。叶子 package 去掉目录已经表达的领域前缀；`httpfetch` 等具体行为名称保留。调用处可用领域明确的别名避免名称冲突，例如：

```go
import (
    sqlitefetch "lerna/adapters/research/sqlite"
    sqlitememory "lerna/adapters/memory/sqlite"
)
```

本次只调整组织与命名，不合并现有 Go package。后续是否合并 module，以减少调用者的组装规则和跨处修改为依据，不以目录或包数量为依据。
