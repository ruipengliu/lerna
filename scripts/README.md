# 可复现参考检查

`scripts/toolchain-lock.json` 锁定本机已验证 Linux amd64 工具及生成器。根 Go module、pnpm workspace 与锁文件继续是依赖合同。SQLc/protoc 制品摘要绑定已验证本地制品；没有将未验证的发布者签名标记为已核。

```sh
pnpm install --frozen-lockfile
python3 scripts/install_generators.py --destination /private/reference-tools
export PATH=/private/reference-tools/bin:$PATH
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
scripts/check
```

安装脚本可用 `--archive-dir /private/downloads` 读取同摘要的离线制品。脚本不会更改 module 或刷新锁。完整工具版本、真实 TZDB 原始字节先经 `check_toolchain.py` 核验，SQL/Protobuf 在临时目录重建并比较，Go/TS/core 字典使用各自只读漂移检查。

`scripts/check` 不接受参数。它运行生成、导航/架构检查、gofmt、vet、全量 Go 测试和 pnpm 格式、lint、类型、测试与 build。Go 包并发固定为 2，避免验收环境过载消耗实际业务窗口；业务命令期限和 Claim 门禁保持原合同。提供私有 `HARNESS_TEST_POSTGRES_DSN` 时追加真实 PG 集成及分进程验证；缺失时明确打印 PG 未选择，不把 SQLite 结果算成 PG 验收。真实 PG 分进程测试使用独立临时数据库，需要受信测试数据库创建权限。

Go 测试 runner 的单包累计上限为 90 分钟；完整 `scripts/check` 可能包括两个 Go 检查阶段、生成和前端检查。CI 的全量 race 单包同样最多等待 90 分钟，job 总上限为 180 分钟。上限容纳同一包内多段真实两库、公开进程和故障验证的累计运行，不延长任何 Task、Command、ControlWindow、Claim 或夹具业务期限。超出 runner 上限仍使检查失败；配置值不构成实际全套完成时间或 hosted CI 通过证据。

默认 `HARNESS_TEST_TZDB_ROOT` 为仓库的 `conformance/testdata/tzdb/2026b`，包含实际 Debian 2026b 原始 TZif、UTC 链接及不可改写的来源/摘要。该固定子集仅包含参考进程使用的四个时区名称和 Etc/UTC。不得用其它数据伪造版本。

GitHub workflow 固定工具/action/image，使用实际 PostgreSQL 17、真实 SQLite及 race 检查，再启动参考 backend/Vite 运行真实浏览器。浏览器不属于默认 `scripts/check`，外部 CI 尚须在平台实际运行；静态 workflow 校验不构成 GitHub 执行证据。

设计检查、参考程序行为、真实外部模型/设备和生产 AZ/容量证据分别成立；本检查入口不以本机通过声明未提供的外部能力。
