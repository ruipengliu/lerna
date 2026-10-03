# Harness

Harness 保存用户目标，依据当前授权推进决策与行动，独立核验目标效果并发布准确 Result。
仓库包含 Go 参考内核、真实 PostgreSQL/SQLite 适配、Go/TypeScript 恢复 SDK，以及 React/Vite 管理界面。

当前可运行闭环是受信报告模板：保存原目标 → 形成条件 → 实际写文件 → 独立读回 →
核验当前条件 → 发布 Result。默认规则引擎只解释闭合模板。可选 HTTP 模型出口具有冻结输入、
真实调用账本与单次物理请求合同，尚无真实供应商账户或通用自然语言质量验收。
完整完成状态见[实施覆盖与证据](docs/architecture/engineering/implementation-coverage.md)；
Search/Body 适配器、完整模拟手机手势与不可信 WASI 仍有本地实现缺口，
真实账户、公司身份、设备与生产规模另待验收。

## 工具与安装

本轮验证环境为 Linux x86_64。依赖版本由 `go.mod`、`pnpm-lock.yaml` 和
`scripts/toolchain-lock.json` 固定：Go 1.26.8、Node 24.19.0、pnpm 11.19.0、
sqlc 1.31.1、protoc 36.2、protoc-gen-go 1.36.12、protoc-gen-go-grpc 1.6.2。
SQLite 驱动使用 CGO，需要 C 编译器。浏览器测试另需 Chromium。
真实数据库证据来自 PostgreSQL 17.11 和持久 SQLite WAL/FULL/foreign_keys 文件。
以下命令以锁定版本的 Go、Node、pnpm 已在 PATH 为前提；
`install_generators.py` 安装并验证 sqlc/protoc，不安装运行时、数据库或浏览器。

```sh
python3 scripts/install_generators.py --destination "$PWD/.local/tools"
export PATH="$PWD/.local/tools/bin:$PATH"
GOBIN="$PWD/.local/tools/bin" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
GOBIN="$PWD/.local/tools/bin" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
go mod download
pnpm install --frozen-lockfile
pnpm build
export HARNESS_TEST_TZDB_ROOT="$PWD/conformance/testdata/tzdb/2026b"
```

安装器可通过 `--archive-dir` 使用已校验的本地安装包。时区规则锁定 2026b；
管理进程需要准确的 tzdb 目录，可通过 `HARNESS_TEST_TZDB_ROOT` 指定测试制品。
生成物分别由 `scripts/generate_core.py`、sqlc 与 SDK 生成器维护，避免手工修改派生文件。

## 本机开发

在仓库根目录构建入口：

```sh
mkdir -p bin
go build -o bin/harness-migrate ./cmd/migrate
go build -o bin/harness-gateway ./cmd/gateway
go build -o bin/harness-application ./cmd/application
go build -o bin/harness-worker ./cmd/worker
go build -o bin/harness-cli ./cmd/cli
go build -o bin/harness-dev ./cmd/harness-dev
export HARNESS_DEV_ROOT="$PWD/.local/harness-dev"
```

显式 SQLite 开发模式：

```sh
bin/harness-migrate --config "$HARNESS_DEV_ROOT/config.json" \
  --development-init --data "$HARNESS_DEV_ROOT" --driver sqlite
bin/harness-dev --config "$HARNESS_DEV_ROOT/config.json"
```

这是一库本机参考装配，不能据此声称设备 SQLite 已接管云端 Task。开发初始化建立私有配置、
身份文件和准确规则；配置固定原 tenant、owner、database_id 和目标目录。
再次执行不会换原身份。生产身份适配未配置时，生产模式拒绝启动。

PostgreSQL 模式需要受控环境预先注入 `HARNESS_DATABASE_DSN`。
连接串与 bearer 不写入仓库或命令正文。使用另一个私有数据目录初始化：

```sh
export HARNESS_PG_ROOT="$PWD/.local/harness-postgres"
bin/harness-migrate --config "$HARNESS_PG_ROOT/config.json" \
  --development-init --data "$HARNESS_PG_ROOT" --driver postgres
bin/harness-dev --config "$HARNESS_PG_ROOT/config.json"
```

普通迁移只运行 `bin/harness-migrate --config /absolute/config.json`。
业务进程读取已迁移的原数据库，不竞相改表；空库或不同 database_id 不能恢复既有 owner。
运行前核对配置中的 `static_dir` 指向本 checkout 的 `apps/web/dist`，
`origins` 包含实际浏览器 Origin，`tzdb_root` 指向锁定字节。
初始化的示例权限、预算和规则只适用于显式开发配置。

启动后打开 `http://127.0.0.1:8080`，在登录页从私有 `token_file` 输入开发凭据。
也可另起 Vite：

```sh
HARNESS_API_ORIGIN=http://127.0.0.1:8080 pnpm dev
```

界面分别呈现接纳、实际效果、Task 完成、Result 导出、费用和清理。
退出页面不会取消 Task；重新登录后沿原身份恢复。

## 可选模型配置

私有 `config.json` 的 `model` 字段可以显式装配 HTTP Engine；省略时使用受信规则引擎。
以下只展示字段形状。endpoint、model、receiver、费率与 tokenizer/framing 合同须替换为
实际账户合同，示例数字不代表已验证的供应商价格。凭据引用环境变量名，配置不保存密钥值。

```json
{
  "model": {
    "profile_ref": {
      "component_id": "model_11111111111111111111111111111111",
      "version": "1",
      "digest": ""
    },
    "endpoint": "https://models.example/v1/chat/completions",
    "model": "your-byte-token-model",
    "receiver": "your-model-endpoint",
    "location": "cloud",
    "credential_env": "HARNESS_MODEL_API_KEY",
    "credential_id": "model-credential-v1",
    "tokenizer_contract": "utf8-byte-upper-bound-v1",
    "input_usd_per_million": "2",
    "cached_input_usd_per_million": "1",
    "output_usd_per_million": "4",
    "billing_final": false,
    "context_limit": 65536,
    "max_input_tokens": 60000,
    "max_output_tokens": 4096,
    "safety_margin": 1024,
    "max_input_bytes": 65536,
    "request_timeout_seconds": 30,
    "max_response_bytes": 65536,
    "max_concurrent": 2,
    "guidance": "仅输出同版闭合 ModelOutput JSON。",
    "allow_http_for_loopback": false
  }
}
```

空 digest 只供首次冻结配置时计算，后续原责任必须保留准确 Profile 与配置。
当前计数器是整份 UTF-8 wire 字节加有限 framing 的上界合同；不声明适配任意模型。
缺凭据或精确费率/计数合同时拒绝装配，不隐式降回其他模型。
模型只能提出闭合草稿；准确 Grant、Task 预算、来源权限、行动准入与独立检查仍分别裁决。
`billing_final=false` 保留未结费用，不由一次 usage 回复推断供应商已结账。
模型整链参考测试已在 SQLite/PG 验证原历史/附件进入准确请求、实际发送前授权/预留、
一次 POST 与真实测试回复 USD 0.00024 在 Task/Grant 两方归并；任务明确 failed，无 Result。
实际断连保留原 Call 与未结费用/预留，重启不发第二次请求。
这些是协议与账务证据，真实供应商最终对账仍缺失；范围见
[模型出口](adapters/providers/README.md)。

## 独立进程与 CLI

以下三个进程使用同一份准确配置，在不同终端运行；迁移已由管理入口完成：

```sh
bin/harness-application --config /absolute/config.json
bin/harness-gateway --config /absolute/config.json
bin/harness-worker --config /absolute/config.json
```

Gateway 保持浏览器连接并向 Application 转交原命令；Worker 推进持久 Job。
一个 Worker 独占本参考装配的 native 文件和三台持久模拟手机目录。
当前同库云端 Worker 的目标宿主不等同于独立设备 Executor；
设备 SQLite、有限 GrantLease 与远端 Authority 的完整部署还未开放。

CLI 的全局选项放在操作前。鉴权使用 `--token-file` 或 `--token-envref`；
完整命令/查询 JSON 由调用者准备，沿原 command_id、profile、Schema 和 TTL 保存与恢复。

```sh
bin/harness-cli --endpoint https://gateway.example \
  --token-file /private/identity-token discover
bin/harness-cli --endpoint https://gateway.example \
  --token-file /private/identity-token --journal /private/cli-journal \
  --request /private/exact-command.json command
bin/harness-cli --endpoint https://gateway.example \
  --token-file /private/identity-token --journal /private/cli-journal recover
```

`query` 使用 `--request`；`receipt` 另需 `--command-id` 与保存原命令的 journal。
TLS 可通过 `--ca-file` 指定信任根；WSS/gRPC 还需 `--discovery` 的 HTTPS 发现入口。
本机明文仅在显式 `--development-loopback` 且数值 loopback 地址时允许。
CLI 不替调用者重写旧 TTL、创建同义新命令或跳过当前权限。

## 检查与证据

```sh
scripts/check
```

该脚本不接收参数，执行工具链/生成漂移、设计文档、Go 格式/vet/测试，以及
pnpm 格式、lint、严格类型、测试和构建。提供 `HARNESS_TEST_POSTGRES_DSN` 时追加真实 PG
集成与进程检查；未提供时明确标为未选择。单独的包与前端检查可直接运行：

```sh
go vet ./...
go test ./...
go build ./...
pnpm generate:check
pnpm fmt:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
```

并发改动增加受影响包的 `go test -race`。PG 领域套件按自己的环境变量选择后端，
例如治理使用 `HARNESS_GOVERNANCE_POSTGRES_DSN`，执行使用
`HARNESS_TEST_EXECUTION_BACKEND=postgres` 与 `HARNESS_TEST_POSTGRES_DSN`；
完整命令见各模块 README 和[覆盖表](docs/architecture/engineering/implementation-coverage.md)。
没有选中 PG 的 skip 不能作为 PG 通过证据。

浏览器需运行真实后端与静态构建或 Vite，单独执行：

```sh
HARNESS_BROWSER_URL=http://127.0.0.1:8080 \
HARNESS_TOKEN_FILE=/private/identity-token \
HARNESS_BROWSER_ARTIFACTS=/private/harness-browser-artifacts \
HARNESS_REQUIRE_CSP=1 \
pnpm test:browser
```

`HARNESS_CHROMIUM` 可指定浏览器。当前环境证据位于
`/workspace/harness-dev-environment/*.json` 与 `/workspace/harness-web-qa/`；
每份记录保留对应实现 commit 和实际测试前提。
固定提交 `877730f` 的完整 Web 流程已通过，覆盖原提交丢回执/reload、澄清、准确 Result、
控制、Surface、Memory、窄屏和登出；Result 首读的费用未结状态保留。
后续审查修复与最终统一检查仍按各自准确提交记录，整体项目保持 partial。

开发规范见 [AGENTS.md](AGENTS.md)，术语见 [CONTEXT.md](CONTEXT.md)，
设计入口见[架构](docs/architecture/README.md)，模块范围见各 `internal/*` 与 adapter README。
