# Atlas 与 Boreal 比较（合成资料示例）

| 维度 | Atlas 1.0 | Boreal 1.0 |
| --- | --- | --- |
| 部署 | 单进程、嵌入式存储 [A1] | 外部数据库、两个 worker [B1] |
| 限制 | 不支持多节点故障转移 [A2] | 支持 worker 替换 [B2] |
| 维护 | 操作者管理备份 [A2] | 操作者维护数据库备份 [B2] |

小型单机使用优先评估 Atlas；需要 worker 替换时评估 Boreal，并承担外部数据库维护。资料没有人时或费用测量，不能断言 Boreal 的总维护成本必然更高。

[A1]: https://atlas.example/1.0/deployment
[A2]: https://atlas.example/1.0/limits
[B1]: https://boreal.example/1.0/deployment
[B2]: https://boreal.example/1.0/limits

这些是虚构产品和合成来源，报告不用于真实软件选型。
