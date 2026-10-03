# 切片02首票：Host入口与演示业务消费者的目录边界

2026-10-03。已读取根AGENTS模块依赖表及 `/tmp/lerna-02-admission-decisions.md`。仅/tmp裁决，不修改repo，不新建领域模型或通用框架。

## 结论

**不采用将record业务逻辑及其Repository消费方放在host/durablework，再让PG adapter导入该包的方案。** 没有Go import cycle只满足技术条件，不能消除职责冲突。

AGENTS明确：`host / cmd`负责组合和生命周期，**不得承担领域状态迁移**；`adapters`实现消费方声明的接口，**不得反向调用宿主**。演示虽然不是Task产品领域，仍包含创建/更新前态、输入revision、固定拒绝和后续Job触发等业务状态规则。这些不能因为叫Host演示就放进宿主装配层。

此前“Host演示入口/Host观察”指本阶段的**内部调用和验收边界**，不意味着所有入口背后的业务实现都必须在host目录。演示可经宿主调用独立消费者，和现有分层完全相容。

## 最小替代：根internal中的专用演示包

推荐把拟建的host/durablework业务消费者直接放到 **`internal/durableworkdemo`**：

- 包内持有当前实际使用的演示输入/观察类型、闭合内部命令验证、record前态规则和revision变更、业务Repository最小接口；依赖contract及必要runtime端口。
- 该包不导入host、cmd或具体PG/SQLite adapter，不建立Task、Grant服务或公开SDK，不称为通用业务框架。
- 演示Repository的PG实现可导入这个consumer包；后续SQLite实现同一接口。数据库SQL和驱动仍留适配实现，业务前态规则留消费者。
- runtime继续只声明和组织共同Tx/CommandStore/JobStore/Claim等持久机制，不导入durableworkdemo，也不理解text/project的业务含义。
- host/cmd或集成测试装配demo、runtime和adapter，注入owner/主体/授权策略/时钟并管理生命周期；不在main或host中重写record状态转换。

根internal的Go可见性允许本仓库host、cmd、adapters及conformance使用，同时明确这不是第三方稳定API。它是有当前真实消费者的单一演示包，不是预建shared/common目录。就近README标明它只用于A阶段无副作用持久工作演示，当前使用者及调用边界。

这项改动最小：消费者接口和业务实现保持同一位置，仅从宿主命名空间移到明确的演示消费者命名空间，adapter相应改import。准确子包/文件名可由实施者统一；建议`durableworkdemo`而非模糊`common`/`service`。

## conformance/internal/testkit为何不是此次默认

如果消费方放 `conformance/internal/testkit/durablework`，顶层 `adapters/postgres` 无权导入它：Go internal规则只允许conformance子树内的importer。不能为通过编译删掉internal标记，继而让生产adapter反向依赖测试基础设施。

该布局只有在**演示业务、演示专用SQL repository和演示可执行harness都留在conformance子树**，顶层adapter只承担可复用runtime存储时才合理。它需要重新安排更多装配/可执行入口，当前无必要。故本次明确选择根internal专用demo包，不在两套布局间保持待定。

## 验证和边界

只需核对实际import图及既有Host/真实PG行为套件：adapter不导入host/cmd；host不实现业务状态转换；runtime不导入demo；所有事实/receipt/Job仍经明确同一Tx，移动包不创建第二事务或复制规则。

不为目录移动新增镜像测试。若PG adapter现有规划把所有业务SQL和状态判断混在同一方法里，仍须保持consumer作业务裁决、adapter作受条件保护的存取，不能借移动目录掩盖职责混合。具体SQL锁实现和私有Tx token沿用前次决定，交实施者选择。
