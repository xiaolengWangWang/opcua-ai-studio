# 程序读取与 OPC UA 功能设计对照

日期：2026-10-14。对应 [后续完整功能设计](2026-10-14-opcua-client-product-design.md)。

## 1. 读取范围

检查了用户指定安装目录 `D:\Program Files (x86)\OPCUA Client Simulator` 中的运行程序、运行时配置、模块名称及实际界面。另从 [Redisant 官方页面](https://www.redisant.cn/opcuaclient) 下载了 Windows 参考包。

安装程序的 ProductVersion 元数据为 `1.0.0+4eab0571ff23730fcd7e7ef6c38aa538645adb62`，主要应用 DLL 大小为 4,478,464 字节；运行时配置为 .NET 8。官网包文件名标记为 `1.0.1.0`。这些版本标记分别记录，不据此推断两份程序的全部行为一致。

同时读取了本项目桌面入口、UI、控制器、OPC UA 适配、类型和值处理、记录器、模拟器、CLI 与 Windows 打包程序，确定现有基线及后续改动边界。

本次提取功能与操作结构，没有将第三方程序代码、DLL、界面素材或许可证系统加入本项目。

## 2. 实际界面观察

| 观察项 | 实际内容 | 对本项目设计的影响 |
| --- | --- | --- |
| 主界面 | 顶部菜单/工具栏、左侧连接树、右侧工作区 | 逐步采用连接树加功能页的组织方式 |
| 工具栏 | 新建、打开、关闭、编辑、删除连接；前后导航；搜索节点 | 连接生命周期与节点导航分开，不将选择节点解释为设备动作 |
| 文件菜单 | 新建/打开/关闭连接、退出 | 按连接状态控制可执行动作 |
| 编辑菜单 | 编辑/删除连接 | 连接运行时编辑与移除有明确限制 |
| 显示菜单 | 保持窗口最前 | 作为后续工作区易用性设置，默认不开启 |
| 连接右键 | 打开、关闭、编辑、删除 | 操作与选中连接绑定；多连接阶段保留独立状态 |
| 新建连接 | 别名、服务器地址、安全模式与策略、认证、获取可用端点 | 连接窗口以用户理解的字段为主，发现端点后选择实际组合 |
| 端点发现 | 本机演示端点返回 None 和 Anonymous | 将发现端点与会话、节点读取分为不同验证步骤 |
| 搜索节点 | 连接、命名空间、标识符类型、值 | 完整 NodeID 粘贴与结构化输入同时支持 |
| 节点图例 | 对象、变量、方法、对象类型、变量类型、引用类型、数据类型、视图 | 节点类别可视化，操作按类别提供 |
| 删除连接 | 对话框明确显示将删除的连接别名 | 破坏性管理操作显示目标对象；本次仅移除临时参考连接 |

本次临时创建的连接为 `OPCUAAIStudio-设计参考-20261014`，端点为自有回环模拟服务。已完成端点发现、连接操作入口和节点定位配置检查；本轮没有完成数据视图中的变量读取、订阅、历史与事件的完整互操作验收。临时连接已移除，本机模拟进程已停止。

界面截图保存在忽略的本地目录 `.test-tmp/reference/redisant-opcua-client/`，包括原生窗口、连接、节点搜索及菜单观察材料。截图只用于本地设计核对。

## 3. 程序模块证据与设计映射

从安装程序的字符串/类型名称中读取了下列模块标识。名称证明程序存在相关模块线索，不独立证明每项协议能力或实际测试结果。

| 模块线索 | 对应能力 | 本项目设计 |
| --- | --- | --- |
| `ConnectionViewModel`、`ConnectionConfigViewModel` | 连接配置与运行状态 | F02～F04 |
| `EndpointDescriptionViewModel` | 端点描述 | F03 |
| `ConnectionTreeView`、`RootNodeViewModel`、`OpcNodeViewModel` | 连接及地址空间树 | F05 |
| `SearchByNodeIdWindow`、`NamespaceViewModel`、`NodeIdViewModel` | NodeID 与命名空间定位 | F05 |
| `AttributeViewModel`、`ReferenceViewModel` | 属性和引用 | F06 |
| `WriteValueAttributeWindow` | 值属性写入 | F12 |
| `DataView`、`MonitorVariablePanelViewModel`、`MonitoredVariableViewModel` | 变量监控视图 | F15 |
| `SubscriptionSettingWindow`、`MonitoredItemSettingsWindow` | 订阅及监控项设置 | F15 |
| `MonitoredItemPlotWindow` | 实时变量绘图 | F16 |
| `ReadHistoryDataConfigViewModel`、`DataHistoryView`、`HistoryDataPlotWindow` | 历史读取与绘图 | F17 |
| `MonitorEventPanelViewModel`、`MonitoredEventViewModel`、`EventView` | 实时事件监控 | F18 |
| `ReadEventHistoryConfigViewModel`、`EventHistoryView` | 历史事件 | F19 |
| `MonitorImagePanelViewModel`、`ImageView` | 图像监控 | F20 |
| `RelationshipDiagramPanelViewModel` | 模型关系图 | F21 |
| `LogViewModel` | 日志显示 | F22 |

安装目录可见 Avalonia、OPC UA、ScottPlot、CsvHelper 等组件文件。这些用于理解参考程序的组织方式，不作为本项目更换技术栈或复制其实现的依据。

方法调用、复杂类型与导出格式等完整支持范围进一步参考厂商公开功能说明，并在本项目各阶段制定独立验收。没有在元数据列表看到某个名称，也不能据此断言参考程序不支持该功能。

## 4. 已有源码与后续设计的对应关系

| 本项目位置 | 已有职责 | 设计上的后续工作 |
| --- | --- | --- |
| `cmd/opcua-ai/main.go` | 桌面入口、生命周期、演示 smoke | 功能页组织、帮助、项目加载及阶段验证 |
| `internal/ui/desktop.go`、`panels.go` | 表格、连接配置、读写、逐层浏览 | 单点独立入口、结果保留、质量筛选、导出、逐步树化 |
| `internal/service/controller.go` | 会话代次、状态、取消、串行操作与记录 | 任务进度与完整性；多连接阶段增加独立会话管理 |
| `internal/service/operations.go` | 读取、写入、独立读回、轮询 | 分批读取、单点结果合并、手动检查与轮询协调 |
| `internal/service/browse.go` | 单父节点浏览 | 递归范围、去重、分页与停止；保留浏览完整性 |
| `internal/opcua/client.go` | Endpoint、认证、安全配置、Browse/Read/Write | 能力检测；逐步增加订阅、历史、方法与事件 |
| `internal/device/device.go`、`value.go` | NodeID、值、质量、时间戳和驱动接口 | 原始质量与执行状态分离；稳定命名空间身份；复杂值 |
| `internal/recorder/` | SQLite 服务级通讯记录及轮转 | 将采样、事件和审计作为不同记录类型；本地回放 |
| `internal/simulator/server.go` | 回环静态六节点演示 | 动态信号、质量/故障与可配置模拟器 |
| `internal/cli/cli.go` | 发现、浏览、读写、JSON 与退出码 | 全量检查、点表核对、筛选导出及可审计报告 |
| `build/windows.ps1` | GUI/CLI/模拟器打包与摘要 | 真实提交号、离线帮助、验证报告及公开发行 |

### 4.1 当前基线暴露的关键设计问题

1. 读取结果被后续读取整体替换，单点复查会丢失批量视图；应按点位身份合并。
2. 点击结果行会把节点清单换成一个点；应将“选中单点”和“编辑批量清单”分开。
3. 单父节点浏览不等于全量点位发现；需要任务范围、递归、去重、完整性与停止。
4. 协议质量与类型不匹配、解析失败混用同一状态，容易把客户端错误误称为服务器 BAD；应分列保存。
5. 每批读取有点数上限；全量任务应分批并适配服务器限制。
6. 清单复制不是文件导出；应冻结快照，说明范围、质量筛选及未完成状态。
7. 本机演示与本工具来自同一协议适配体系，不能替代独立客户端和不同服务器的互操作验收。

## 5. 设计输出

据此形成 [32 项后续完整功能设计](2026-10-14-opcua-client-product-design.md)：首先交付帮助、单点验证、全量读取、GOOD/BAD 导出；再按浏览/点表、多连接/安全、订阅/趋势、复杂值/方法、历史、事件/模型、模拟器等阶段演进。

OPC UA Server、Pub/Sub、Classic DA、RFID/AutoID 与数据对接单独列为扩展路线，分别验证实际协议库、运行环境和互操作样例。当前阶段为设计交付，功能实现和正式发版在设计确认后的实施阶段继续。
