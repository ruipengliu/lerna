# Ticket02：TS编解码实现与公共入口的依赖方向

2026-10-03。已查看 `/tmp/lerna-worktrees/contract-02/sdk/typescript/src/index.ts`、`commands.ts`及包配置。

## 决定

立即实施最小局部拆分：将index.ts中的codec实现搬至codec.ts；index.ts成为纯公共重导出入口；commands.ts从codec.ts直接导入decode/version。

实际现状为 `index -> commands -> index`。当前初始化后才访问函数，所以测试能够通过，但后续digest/query若增加初始化工作，循环会使初始化先后成为隐含约束。这个依赖已经存在且拆分成本小，无需等切片完成后积累。

## 具体边界

- codec.ts持有Ajv单例、format注册、schema注册、私有isWireValue，以及原version/validate/decode/encode实现。
- codec.ts直接导入json.ts与generated/values.ts，不导入index.ts或commands.ts。
- commands.ts直接导入codec.ts与generated/values.ts；行为、错误码、验证顺序不改变。
- index.ts只显式重导出原公开项：json.ts的parseJSON/maxBodyBytes/maxDepth；generated/values.ts现有全部公开项；codec.ts的version/validate/decode/encode；commands.ts的ContractError/parseCommand/decodeCommand。
- package.json的exports继续为 `./dist/index.js`；不增加codec子路径公开承诺，不改变函数签名或错误语义。
- 后续digest/query等实际实现直接依赖所属实现模块，不能反向导入公共barrel index.ts；外部调用方与公开验收继续从index.ts导入。

这是代码组织细化，不需ADR，不新增接口、通用框架或镜像实现。version暂随codec.ts原样移动；当前不为一个常量预建独立模块。

## 验证

复用已有公开Go/TS合同suite（当前报告109夹具/13个TS测试）、TS类型检查及build，确认公共facade消费方式仍然成立；检视内部模块导入方向无循环。无需为搬移新增镜像测试，也不要把当前suite曾通过当作搬移后的已验证结果。

本代理仅作决策，已直接通知ticket02实施代理执行；未改产品代码。
