# UML 图册修订渲染记录

检查日期：2026-09-28。

## 源文件

| 文件 | SHA-256 |
|---|---|
| `docs/architecture/diagrams/uml-models.drawio` | `851ece71759fb44663ca2d17912bcc0004e1c108991894c632150cda73880046` |
| `docs/architecture/diagrams/architecture-atlas.html` | `163bc010450cd5f18dea2e507ee4dafb51d349c3240e78e00bb68e5b80f0bc2e` |

## 证据及核对

- [drawio-18-evaluation.png](drawio-18-evaluation.png)：draw.io CLI 导出第 18 页，宽 2000 px。已目视核对 `plan_id {unique}`、Plan→Run 的 `1→0..1`、`rollback_approval_ref` 和页底的独立旧版批准规则，未见截断、遮挡。
- [drawio-20-extensions.png](drawio-20-extensions.png)：draw.io CLI 导出第 20 页，宽 2000 px。已目视核对回退批准字段及页底恢复规则，未见截断、遮挡。
- [atlas-18-evaluation.png](atlas-18-evaluation.png)：Playwright Chromium，1600×1200 viewport，经 localhost 打开 HTML，展开关系表后截取 `#view-18`。已目视核对整体 Run 字段、R01 单 Plan 最多一个 Run、多重性、R08 独立旧批准及说明文字，未见截断、遮挡。
- [atlas-20-extensions.png](atlas-20-extensions.png)：同一浏览器截取已展开关系表的 `#view-20`。已目视核对回退引用字段及独立核验规则，未见截断、遮挡。

浏览器唯一控制台错误是未提供 favicon.ico 返回 404；页面自身未报告脚本错误。上述证据验证排版与已显示的规则，不验证真实服务、数据库恢复、预览取得或隔离环境行为。
