# 手机执行路径与电脑宿主条件调查

调查日期：2026-09-09。状态：真实设备背景研究，未运行设备实验；当前参考验证采用多模拟设备。

对应 [目标基线](../harness-project-goals.md) 的 C4、C5、C6、C7、C8 与 V2；本文使用根目录 [领域词汇表](../../CONTEXT.md) 中的运行节点、执行系统和插件等术语。

整合说明：用户已明确无需验证真实设备，本期构建多个模拟设备验证系统能力。以下真实平台路径与“需实测”事项仅在未来声明真实设备支持时适用，不构成当前能力验收的前置要求。

## 结论与适用范围

Android 的 AccessibilityService 与 ADB/UI Automator、iOS 的 XCTest/Appium XCUITest 都有可用于观察和操作的接口，但安装条件、控制权限及宿主依赖不同。接口存在只能证明候选路径可调查，不能证明任意应用、日常设备或长时间跨端任务已可可靠执行。

需要分别决定“可接受什么设备条件”和“选哪个驱动”。尤其不能把 Android 系统返回、iOS 应用内导航返回、回到主屏幕合并成无差别动作；也不能把终止驱动会话当成已完成用户接管。

本调查仅覆盖手机执行和电脑作为运行节点或自动化宿主的条件，不扩展到完整桌面 GUI 自动化、设备采购或应用商店发布方案。

## 路径可行性矩阵

表中“支持”表示官方接口或文档给出该能力；目标应用覆盖率、操作成功率与中断延迟全部待实测。

| 路径 | 截图与结构化观察 | 点击、滑动、输入、返回 | 手机上需要什么 | 电脑及连接条件 | 日常设备边界 |
| --- | --- | --- | --- | --- | --- |
| Android AccessibilityService | 窗口无障碍节点；API 30+ 可获取截图，需声明截图能力；安全窗口和权限不足可能失败。 | 节点动作、API 24+ 手势、支持节点上的设置文本、系统返回。 | 安装包含服务的配套应用，由用户启用服务；该路径本身不以 ADB、开发者选项或 Root 为前提。 | 观察和动作可在手机执行；与其他节点连接由 Harness 实现，不要求电脑持续运行 ADB。 | 接入较接近端侧运行；应用暴露的节点和动作范围不同。Google Play 的自主自动化限制直接影响分发可行性。 |
| Android ADB + UI Automator | ADB 截图；UI Automator 获取界面层级、查找元素、截图。 | UI Automator 的点击、滑动、文本操作和系统返回。ADB 在此承担连接与测试启动，不能把 ADB 本身当完整语义驱动。 | 启用调试并信任宿主；部署 instrumentation 测试程序。官方基础流程不要求 Root。 | Platform Tools；构建测试程序还需匹配的 Android 构建环境。USB 调试或符合条件的无线调试。 | 可使用已授权真机，但属于测试基础设施；不能假定日常用户愿意保持调试及测试进程。 |
| iOS XCTest / Appium XCUITest 默认流程 | XCUIElement 查询与快照、截图；Appium 通过 WDA 包装 XCTest。 | 点击、滑动、文本输入；“返回”需要映射到具体应用导航或手势，不能承诺 Android 式全局 Back。 | 测试运行器；Appium 需签名并安装 WDA，信任电脑，iOS 16+ 开发者模式及 UI Automation 设置。无需越狱路径。 | 完整默认流程依赖 macOS、Xcode；Appium 另需兼容的 Server/Driver。真机先完成配对；无线运行条件需按版本核实。 | 是开发测试路径，不等于安装普通 App 后即可控制任意其他 App；日常配置可能被测试驱动调整。 |
| Appium XCUITest 非 macOS 宿主 | 沿用真机 WDA 的观察和动作能力，不能推导所有命令均可用。 | 同上，另受非 macOS 命令路径限制。 | 当前文档要求 iOS 18+ 真机；预装有效签名 WDA 或外部管理的运行中 WDA。 | Windows/Linux 有限支持；RemoteXPC 环境、显式设备 ID 与系统版本；不能在该宿主构建 WDA，不能运行 iOS 模拟器。 | 可减少运行时 Mac 依赖；不能据此删除构建签名、配对、会话管理等前提。 |

Android 服务行依据 [服务开发指南](https://developer.android.com/guide/topics/ui/accessibility/service)、[服务 API](https://developer.android.com/reference/android/accessibilityservice/AccessibilityService) 和 [节点动作 API](https://developer.android.com/reference/android/view/accessibility/AccessibilityNodeInfo.AccessibilityAction)。ADB/UI Automator 行依据 [ADB](https://developer.android.com/tools/adb)、[UI Automator 指南](https://developer.android.com/training/testing/other-components/ui-automator) 和 [UiDevice API](https://developer.android.com/reference/androidx/test/uiautomator/UiDevice)。iOS 行依据 [XCUIElement](https://developer.apple.com/documentation/xcuiautomation/xcuielement)、[开发者模式](https://developer.apple.com/documentation/xcode/enabling-developer-mode-on-a-device)、[Appium 环境要求](https://appium.github.io/appium-xcuitest-driver/latest/getting-started/system-requirements/)、[设备准备](https://appium.github.io/appium-xcuitest-driver/latest/getting-started/device-setup/) 和 [非 macOS 宿主](https://appium.github.io/appium-xcuitest-driver/latest/guides/non-macos-hosts/)。“不要求 Root/越狱”仅指上述标准接入流程没有该前提，不能理解为绕过目标应用或系统保护。

## 影响设计的具体事实

### Android：本地服务与调试宿主是不同接入模式

AccessibilityService 需要声明窗口内容读取和手势等能力。手势派发会取消当前进行中的手势，包括用户或其他服务的手势。因此 Harness 必须设计动作互斥和用户接管，不能默认与手动操作无冲突地同时运行。`onInterrupt` 描述的是服务反馈中断，也不能直接作为 Harness 任务取消完成的证据。[服务指南](https://developer.android.com/guide/topics/ui/accessibility/service)、[dispatchGesture API](https://developer.android.com/reference/android/accessibilityservice/AccessibilityService#dispatchGesture(android.accessibilityservice.GestureDescription,%20android.accessibilityservice.AccessibilityService.GestureResultCallback,%20android.os.Handler))

设置文本应调用目标节点支持的 `ACTION_SET_TEXT`；它不是对任意控件都生效的输入通道。截图接口从 API 30 起提供，并有截图间隔过短、无访问权限、安全窗口等错误。结构化观察与截图应独立声明可用性。[节点动作](https://developer.android.com/reference/android/view/accessibility/AccessibilityNodeInfo.AccessibilityAction#ACTION_SET_TEXT)、[服务截图 API](https://developer.android.com/reference/android/accessibilityservice/AccessibilityService)

USB ADB 需要开启开发者选项中的 USB 调试，并由用户接受电脑 RSA 密钥。Android 11+ 的无线调试需要配对；官方基本流程要求同一无线网络。手机到电脑的调试连接与 Harness 端云连接是两段独立连接；不能用“云端网络正常”代表“手机执行驱动可用”。[ADB 连接说明](https://developer.android.com/tools/adb)

Google Play 当前明确限制使用 Accessibility API 自主发起、规划和执行动作或决策；确定的人类预设规则自动化与经验证、以服务残障用户为核心用途的辅助工具有不同规定。通用助手不能仅因可以帮助部分残障用户就视为该例外。**API 可实现与能按预期用途通过分发渠道发布，是两项不同结论。** 后续平台票必须明确分发范围，不能将受限用途包装成辅助功能工具。[Google Play AccessibilityService 规则](https://support.google.com/googleplay/android-developer/answer/10964491?hl=en)

### iOS：测试运行条件进入节点能力声明

Apple 的 XCUIElement 提供元素查询、点击、滑动和文本输入等测试操作，并支持截图相关协议。它描述测试环境中的控制接口，没有给出“普通第三方 App 获得跨 App 通用控制权限”的承诺。应用内“返回”应由适配器根据可观察导航控件实施并验证。[XCUIElement](https://developer.apple.com/documentation/xcuiautomation/xcuielement)

Appium 要求真机安装有效签名的 WebDriverAgentRunner，完成信任、开发者模式与 UI Automation 配置；驱动会调整部分键盘偏好。开发者模式启用涉及用户重启与确认。因此日常手机支持不能仅凭测试机运行成功认定；应记录被调整的设置，并验证会话结束后的行为。[设备准备](https://appium.github.io/appium-xcuitest-driver/latest/getting-started/device-setup/)、[Apple 开发者模式](https://developer.apple.com/documentation/xcode/enabling-developer-mode-on-a-device)

版本组合需要同时核对 iOS、Xcode、macOS、XCUITest Driver、WDA、Appium。官方当前矩阵注明 Driver 10+ 使用 Appium 3；旧系统有“可能工作但未测试”的组合，不能与完整支持等同。[版本与环境要求](https://appium.github.io/appium-xcuitest-driver/latest/getting-started/system-requirements/)

截至本次查阅，非 macOS 文档明确给出有限 Windows/Linux 支持：仅 iOS/tvOS 18+ 真机、RemoteXPC 依赖、显式设备选择、预装或外部运行的 WDA。不能使用默认 xcodebuild 启动流程；构建 WDA 仍需 Xcode。该路径应独立建兼容性记录，不从默认 Mac 流程继承验收结论。[非 macOS 宿主](https://appium.github.io/appium-xcuitest-driver/latest/guides/non-macos-hosts/)

## 电脑节点需要承担什么

以下是基于上述事实的架构推导，尚未锁定实现：

- 电脑只作为运行节点时，应满足 Harness 选定运行时、持久化与网络条件；是否需要 Android SDK 或 Xcode 取决于它是否托管对应驱动。
- 电脑托管 Android 调试路径时，承担设备识别、ADB 连接状态和测试进程生命周期。操作系统支持与 USB 后端条件按 Platform Tools 版本记录。
- 电脑托管 iOS 默认路径时，承担工具链与签名运行条件；Linux 云端仍可经 Harness 协议请求 Mac 节点执行。不要将云端部署要求误写成全系统必须使用 Mac。
- 每个设备执行入口需要统一授权检查、控制权归属与动作记录。UI Automator、ADB 或 WDA 会话本身不能证明已实现 C7 的委派收缩、用途限制或取消语义。

## 与 V2 的差距及必要实测

所有路径仍需以下实验。测试结果必须记录真机型号、OS 构建号、驱动与依赖版本、安装方式、授权条件、连接方式和目标应用版本。

| 验收面 | 必须回答的实测问题 | 输出证据 |
| --- | --- | --- |
| 观察 → 动作 → 复查 | 同一状态下能否取得截图/节点；界面旋转、键盘和应用切换后坐标及节点是否失效？ | 前后观察及时间、定位依据、动作记录、效果判断。 |
| 点击、滑动、输入、返回 | 中文、特殊字符、密码控件、WebView、自绘 UI 是否支持；iOS 导航返回的适用边界是什么？ | 分动作与控件类型的支持矩阵，明确不支持和未知。 |
| 中断与接管 | 停止按钮能否阻止下一动作；已派发手势能否停止；用户操作何时被检测；接管后是否仍有残余命令？ | 取消请求、最后动作、驱动停止与控制权转移的时间线。 |
| 断连与结果不明 | 动作发生后回执丢失、USB 断开、无线切换、宿主重启时如何核对外部状态？ | 不盲目重试的证据；无法核对时保留不确定结果。 |
| 权限与设备状态 | 服务被关闭、调试授权撤销、设备锁定、签名过期、受保护窗口、后台进程退出时行为是否正确？ | 明确拒绝/等待/失效状态，不冒报执行成功。 |
| 日常使用 | 与用户触摸、通知、电话、键盘偏好、系统省电及常用应用共存时影响多大？ | 日常配置与测试配置的对照记录及恢复检查。 |
| 兼容与互操作 | 相同动作契约在两类驱动上能否表达能力差异，而不假造全平台一致支持？ | 共同契约测试、平台扩展字段、不同实现的结果比较。 |

执行系统返回“动作接口完成”，大脑系统根据后续证据判断“任务结果已确认”；不能把前者直接提升为后者。具体控制权机制、动作状态、超时、恢复协议和首期支持矩阵由后续决策票确定。本次没有性能、成功率或全天运行的实测数据。

## 来源与版本记录

以上链接均为平台或项目官方来源，访问日期均为 2026-09-09。Android 服务 API 的已核对版本锚点：手势 API 24、截图 API 30；文本动作 API 21。ADB 页面标注更新于 2026-09-02；服务开发指南标注 2026-04-17。UI Automator 指南当前包含 2.4 系列预发布示例，本文没有将示例版本选定为依赖。

Appium 使用 `latest` 文档快照：环境要求页面标注 2026-08-25，非 macOS 宿主页面标注 2026-07-09；它们不是本项目锁定的驱动版本。Apple 文档没有在本次抓取中提供可用的页面版本号。部分 Android 大型 API 页全文抓取超时，相关条目通过官方页面搜索索引与官方 API 摘要交叉核对；实施前需按选定 SDK 再确认签名和兼容范围。

事实调查已能支持平台条件讨论；仍缺首期设备与分发约束、具体依赖锁定组合、iOS 无线真机会话与用户接管的验证，以及全部 V2 实测证据。
