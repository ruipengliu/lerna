# 04票02：root七项AC接受

2026-10-04，root正式接受固定交付6510364763f60c7efdf8ff10dde590f3fd607cb8的全部七项AC。最终双轴0硬规范/1可选P3 KEEP、Spec a0/b0/c0，架构0必要新增。

| AC | 实际资格 |
| --- | --- |
| 1 五动作独立 | 当前完整subject/ref/purpose/revision/期限、六flag组合与七非法集合、真实PG锁后clock及拒绝/正常对照；read不推其他许可。 |
| 2 完整来源闭包 | A/B→M→N保留隐藏中间版本、共享祖先去重、循环/fullRef拒绝；完整64/65与最终Get原60秒race case54.75s，未截断；同tenant/owner及明确跨界拒绝。 |
| 3 用途与原期限 | 全来源用途交集、原cap/最严期限、真实旧0001过期祖先、重开及继续派生不可复活；新的合法版本正常完成。 |
| 4 最终当前门禁 | 实际Tx外Put后隐藏祖先process撤销阻出版；调度后准入/alias late gate回滚首Tx，精确marker才允许第二短Tx；原key winner和提交未知独立核验。 |
| 5 耐久清理责任 | policy、原watermark/deadline及原Job同Tx；原保存主体/用途、全部祖先独立准入义务、有限页/重开/真实写拒绝、自然阶段交接；pending不当作物理删除。 |
| 6 隔离与正常对照 | fullRef/current授权在存在性/元数据之前；source Record锁后expiry gate；不获准主体拒绝并保留真实获准派生/读取；外部sources仅声明史。 |
| 7 真实持久接口 | Content/Command与受信Manager入口、真实PG+独立对象、三个原0001 archive/升级及重开；fixture不是Grant。 |

完整170 Component每模式各一次，五个normal和五个race组均实际native0/groupAbsent。Contentfixture/local/PG rows normal/race、Decision20.942/27.024、完整base race、locked check37Node/44TS及158+89+101两序双向、module verify均0；check部分旧Go子调用cached如实保留。详见既存ticket-02-evidence.md及最终归档原文。

281groups/293PIDs/1401schemas/161holders/972身份ACK roots及70旧无ACK路径的audit保留原cutoff：5 schemas/8 roots保留，原Z2279009和空selector停止的per-Close unknown不因后续成功清除。无cleanup或全环境零的声明。Owner已明确无pending native并释放唯一LOCAL槽，全部overlay/WT/日志继续保留。

15/41子AC不等于whole04退出；完整1.2广告仍OFF。Snapshot/物理清理/第二holder/fence/SIGKILL分别由后票交付；准确新push CI待独立核验。
