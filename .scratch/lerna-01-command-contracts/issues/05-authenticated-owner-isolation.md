# 05: 受信身份与固定 owner 隔离

**What to build:** 获准调用方通过公开查询入口读取原命令，跨租户或冒用主体的请求被安全拒绝；目录中的进程地址变化或查询失败不会把原请求转移给另一逻辑 owner。

**Blocked by:** 04 — 固定回执与只读查询。

**Status:** resolved

- [x] 查询入口显式接收受信认证上下文，由其提供租户、主体与委托链；命令 target 或 payload 不得覆盖这些事实，也不能通过自报 owner 获得权限。
- [x] 对原命令引用、目标租户、负责方与受信上下文进行一致性和权限检查；缺少可信身份或越权时返回既定合同拒绝，不能默认采用匿名高权限主体。
- [x] 共同夹具覆盖合法主体正常查询，以及跨租户 target、payload 身份冒用和未经许可的查询；每种拒绝均配正常对照，不能通过全部拒绝满足隔离要求。
- [x] 无权调用方对对象存在与不存在得到不泄露存在性的拒绝或 redacted 视图；原决定、对象内容及异步进展不通过错误细节泄露。
- [x] 调用前固定逻辑 owner；可控受信目录更新同一 owner 的进程地址后仍查询原身份，更新默认 owner 只影响新工作，不迁移原命令。
- [x] 原 owner 不可用或返回 not_found 时保留原引用和期限，返回相应查询结果；不得自动更换 owner、延长 accept_before 或新建同义命令。
- [x] Application／Component 的公开入口验证上述行为，并保留只读约束；证据只描述注入认证上下文及可控目录的范围，不声称已实现真实认证服务或分布式传输。

## Scope

本任务实现合同入口的身份约束和固定 owner 行为。真实网络身份认证、凭据基础设施与分布式服务接替留给相应后续切片。

## Comments

2026-10-03：依赖 01–04 已合入实现集成分支。交付 Go `contract.GetCommand` 与 TS barrel 导出的 `getCommand` 公开 Application / Component 入口，消费方最小 `ReadAuthorizer` / `OwnerDirectory` 端口先裁决准确原引用，再核验并读取固定 owner 的事实。

测试先对未实现入口取得 red，再得到正常读取与不泄露存在性的拒绝 green。后续探针在 Go 与 TS 均复现认证回调改写调用方原始字节导致查询迁移 owner 的 red；入口在回调之前固定独立请求快照后恢复 green。Go 受信委托 slice、TS 主体和引用回调副本以及返回观察均与宿主／事实源隔离。

共同 `conformance/fixtures/1.0.0/queries.json` 有 28 个公开读取场景，两语言运行同一预期：合法主体与委托、存在／不存在的相同拒绝、跨租户与 payload 冒用、授权失败、授权拒绝时目录／事实故障不泄漏、原 owner 错配／不可用、gone、not_found、读取开始截止。另有公开行为测试验证同 owner 的物理 Reader 接替、默认 owner 改变时新读可显式选新 owner 而原命令仍固定原 owner、原截止与事实快照不变、缺少 Go deadline、取消、TS 单总计时器与迟到 reject。

验证：`make check` 通过（Go format / vet / test、TS strict 类型／格式／27 个测试、生成零差异、153 个共同编解码夹具双向 Go↔TS、摘要与受信读取套件、双语言构建）；`make test-race` 通过。新增共同拒绝场景后再次运行 Go / TS 受信读取套件与 `make lint`。合同版本保持 `1.0.0`，未扩宽旧方法 Schema；环境使用仓库锁定 Go 1.27.1、Node 24.19.0、pnpm 12.8.1 / TypeScript 7.0.2。

证据范围：受信主体由 Host fixture 注入，目录与读源受控，不含真实认证／委托签名服务、持久账本或网络传输。Go 有限完成依赖端口遵守 context；TS race 结束异步等待，不能停止不合作回调或同步阻塞。不得将这些本地证据作为生产权限或分布式恢复结论。
