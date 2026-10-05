# a9bc726 的准确 CI

[运行 37288736357](https://github.com/ruipengliu/lerna/actions/runs/37288736357)的 contracts、durable-admission 两个 job 和全部步骤实际成功。准确 head 为 `a9bc726338040d4bf59e6d324691012a0011e1db`。

基础检查包含格式、vet、生成一致性、37 项 Node、44 项 TS、Go 全模块和 158／89／101 共用夹具两序往返及构建。普通与 race 集成均完成 Recovery、201 个 Component（1 closure／97 Content／60 durable／43 other）以及三个 fixture 包。Content 仍为原 49／48 两组；race 第二组实际 119.884s，通过原 120s 包期限。

[准确范围与步骤](observed.json)及两份脱敏日志逐字节归档。Root 核对完整有限包结果及方法范围，未声称亲读服务启动的每行输出。令牌、DSN 和服务密码已脱敏；[原副本哈希](archive-map.json)指向对应脱敏副本。

当前 Run 取消、固定三组和政策时钟候选未包含在此 head。此成功不替代它们自己的新源码资格。旧 75f0429／7f656d9 竞态超时及本地首次三组 race 的旧 Run failure 均保留；04 仍 22/41，完整 1.2 profile 关闭。
