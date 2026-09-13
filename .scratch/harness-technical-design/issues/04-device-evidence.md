# 核实手机与桌面执行的可实现范围

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Resolution: out-of-scope
Assignee: device_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

Android 与 iOS 的官方或主流开源自动化路径，在真机观察、点击、滑动、输入、返回、中断接管、配套应用及主机依赖上有哪些实际能力与限制？桌面节点需要满足哪些宿主条件？

## Scope

核对 Android AccessibilityService、ADB/UI Automator、iOS XCTest/Appium XCUITest 等适配路径；区分测试设备和日常手机、开发者模式和 Root、截图与结构化观察。只研究事实，不选择用户设备。

## Expected evidence

形成平台、动作与观察能力、权限和宿主依赖矩阵，说明与 V2 的差距及必须实测的问题。

## Output

计划关联文档：`docs/research/harness-device-execution-options.md`。

## Comments

研究由 device_research 在独立 worktree 完成。研究分支：`research/harness-device-execution`；提交：`14b61ddb10c849fd5cadf92cb2a9a5309fd2bc4b`。

## Answer

用户在建图阶段明确：本项目通过多个模拟设备验证能力，不要求真实设备测试。首版真实手机平台及控制路径选择因此退出本地图的实施前置条件；本票按范围调整关闭，不作为已选定设备方案。

已完成的 [手机执行路径与电脑宿主条件调查](../../../docs/research/harness-device-execution-options.md) 保留为未来真实平台 Adapter 的背景资料，不构成当前项目必须进行真机验证或选择 Android/iOS 的要求。当前模拟执行设计由“确定多模拟设备与工具执行的验证方案”处理。
