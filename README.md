# OPC UA AI Studio

参考 [S7 AI Studio](https://gitee.com/sun_xuanqi/s7_ai_studio) 创建的独立 OPC UA 桌面调试工具，使用 Go + Fyne。当前版本为 `0.6.0`。

Windows 发行包见 [Gitee Release v0.6.0](https://gitee.com/xiaolengwangwang/opcua_ai_studio/releases/tag/v0.6.0) 或 [GitHub Release v0.6.0](https://github.com/xiaolengWangWang/opcua-ai-studio/releases/tag/v0.6.0)，更新说明见 [版本说明](docs/releases/v0.6.0.md)。发布页提供三种下载：桌面程序单文件 `OPCUAAIStudio-<版本>-Windows-x64.exe`、命令行工具包 `opcua-tools-<版本>-Windows-x64.zip`（opcua-cli 与 opcua-sim），以及包含全部文件的 `OPCUAAIStudio-<版本>-Windows-x64.zip`（程序内更新使用）。

## Windows 桌面

构建后的 `OPCUAAIStudio.exe` 可直接运行，无需安装 Go。界面包含点位与读写、节点浏览、通讯与日志、AI 诊断四个页面。点位页左侧为分组操作栏，右侧结果表占满窗口高度、表头固定，按质量着色（GOOD 绿、BAD 红、UNCERTAIN 橙），可写点位蓝色标出；配色为工业蓝主题，跟随系统浅色或深色。

程序启动后在后台检查 Gitee（失败时改用 GitHub）的最新版本，发现新版本时状态栏显示“新版本”按钮，也可点击“检查更新”手动检查。确认后下载发行包，按发布页的 `SHA256SUMS.txt` 校验，替换安装目录中的程序文件并重启；安装目录不可写或校验失败时不改动现有文件。

1. 点击“本机演示”，读取 `ns=2;s=Counter`，初值为 Int32 42。
2. 输入 123，勾选“允许写入”，点击“写入并读回”，确认 Endpoint、NodeID、类型和数值。
3. 查看 `CONFIRMED` 和独立读回“一致”。
4. 节点浏览页从 `i=85` 浏览 Objects，进入演示命名空间；点击变量追加到清单并填入单点输入。
5. 点击“全量读取”，演示父节点 `ns=2;i=85` 包含九个变量，其中三个为中文点位（如 `ns=2;s=热力站.一次供水温度`）；可停止并保留部分结果。
6. 单点验证 `ns=2;s=Missing`，得到 BAD，同时保留其他 GOOD 点；筛选并导出 CSV。
7. 节点浏览页勾选“选择”列可多选或全选节点，点击“读取选中节点”一起读取。
8. “导出属性表”生成 viSCADA 设备类属性表（全部 / 仅 GOOD / 仅控制点位），填写属性标识、属性名称、读写模式和数据类型；“导入属性表验证”按属性标识读取并筛选 GOOD、BAD、不存在、可写点。
9. 连接区“⋯”菜单中的“连接数测试”逐个建立会话，测出服务器同时接受的会话数后全部关闭。
10. 筛选“含中文”只显示 NodeID 含中文的点位；“导出属性表”可选“仅含中文点位”。
11. 结果区输入关键字模糊搜索（多个关键字空格分隔）或精确搜索，“导出 CSV”和“导出属性表”按当前筛选和搜索导出；“视图”切换树状，按浏览路径或 NodeID 层级查看数据。
12. 单点验证输入 viSCADA 属性标识（如 `2::s::Counter::DINT`），查看属性标识校验和建议属性标识；读数正常后“设为基准点位”，此后每次连接自动检测。
13. 节点浏览页“搜索节点”在父节点下按名称或 NodeID 查找节点，结果可勾选读取。
14. 结果区点击表头排序，勾选“自动刷新”按间隔原位更新结果中的全部点位（刚变化的值以蓝色标出）；左上角折叠按钮收起左栏，让数据区占满窗口。
15. “通讯与日志”页按全部 / 异常 / READ / WRITE / BROWSE 筛选、按 NodeID 搜索请求记录，选中一条查看全部目标和结果，统计响应耗时，可导出 CSV。

单点验证使用独立输入，结果选择不覆盖批量清单。全量读取按 50,000 点设计（上限 60,000 个变量、100,000 个节点）：浏览一次请求多个节点，读取首批 100 点、响应快时加大到每批 5,000 点，单条消息控制在约 0.5 MB，服务器拒绝或超时自动减半，可随时停止。读取同时取得数据类型和读写权限，“读写”列和“可写（控制点位）”筛选标出控制点位。导入、导出和 CSV 在后台运行并显示进度，可随时取消；结果表按需渲染，50,000 点时仍可流畅查看。原始质量 GOOD/BAD/UNCERTAIN 与请求、解析、类型错误分别记录；未收到 DataValue 的质量为 UNKNOWN。CSV 保存冻结快照，包括范围、任务 ID 和完整性，兼容中文并保护 64 位标量整数。完整离线说明见 [操作手册](assets/help/points.md)，也可在程序中点击状态栏“操作手册”。

连接现场服务器时输入完整 Endpoint，如 `opc.tcp://192.168.1.10:4840`，或点击“搜索服务器”扫描本机和局域网网段的常用 OPC UA 端口（4840、4841、4862、4890、48010、49320、53530、62541），点选找到的服务器。点击“发现端点”列出服务器提供的策略和认证方式，点选一个即填入安全策略和模式，其余参数在“安全与认证”中设置。常用连接可“保存配置”（不保存密码），连接后“服务器信息”显示产品、版本、状态、命名空间、会话诊断和已连接客户端。支持匿名和用户名认证；加密连接须提供客户端证书、RSA 私钥及可信服务器证书，服务器证书按完整 DER 内容固定比对，不自动降级。

读取默认 Auto 自动识别类型；写入须明确选择数据类型。一行一个 NodeID，字符串标识保留大小写、空格和逗号，可以包含中文；中文输入法输入的全角 `＝`、`；`（如 `ns＝2；s＝热力站.温度`）自动转换为半角，标识本身保持不变。数组写入设置元素数量，并使用 JSON，例如 `[1,2,3]`。String 支持中文；ByteString 输入十六进制；DateTime 使用 RFC3339，例如 `2026-10-13T08:00:00Z`。

默认只读，每次 GUI 写入都需要确认，并绑定当前连接；连接变化后旧确认失效。`UNKNOWN` 表示最终状态未知，不自动重发；服务端确认与读回一致分别显示。Endpoint 可只填 IP 或主机名，连接时补全为 `opc.tcp://<主机>:4840`，下拉列表保存最近 10 个连接。“断线自动重连”默认开启：服务器断开或连续 2 次无响应（心跳每 5 秒）时按 2–30 秒间隔重连，保留结果并恢复轮询；重连后须重新勾选“允许写入”。

连接输入和点位保存为独立首选项，密码、API Key、写入值和“允许写入”不保存。SQLite 记录位于 `%AppData%\OPCUAAIStudio\records`，按天 / 50 MB 轮转；正常关闭等待记录排空并释放演示端口。记录内容是服务级请求、结果、NodeID、状态码及时间戳，首版不记录原始线缆字节。应用日志位于 `%AppData%\OPCUAAIStudio\app.log`。

AI 诊断沿用 DeepSeek 客户端。填入 API Key 或设置 `DEEPSEEK_API_KEY`，冻结并预览证据后点击分析，将预览的 Endpoint、节点值和状态发送到 DeepSeek。AI 仅生成解释，不执行设备操作。模型名称可修改，默认沿用参考项目配置；AI 网络调用不属于本机演示测试。

## CLI 与模拟器

使用 `go.mod` 指定的 Go 工具链：

```powershell
go build -o bin/opcua-cli.exe ./cmd/opcua-cli
go build -o bin/opcua-sim.exe ./cmd/opcua-sim
```

第一个终端启动演示：

```powershell
.\bin\opcua-sim.exe -endpoint opc.tcp://127.0.0.1:4840
```

压测时加 `-tags 50000`，在 `ns=3;s=Load` 下生成 500 个站点文件夹、共 50,000 个 Double 点：每 10 个有 1 个可写，每 50 个有 1 个返回 BadNoCommunication。本机测试中，新启动的模拟器上全量浏览、读取 50,000 点及其类型和权限用时约 3.5–7 s；模拟器基于 gopcua 服务端，长时间运行后自身会变慢。

另一个终端执行：

```powershell
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 endpoints
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 browse i=85
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 browse 'ns=2;i=85'
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 -json read 'ns=2;s=Counter' 'ns=2;s=Temperature'
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 -type Int32 -allow-write write 'ns=2;s=Counter' 123
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 -type String -allow-write write 'ns=2;s=Message' '供水正常'
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 -type Int32 -count 3 -allow-write write 'ns=2;s=Samples' '[4,5,6]'
.\bin\opcua-cli.exe -endpoint opc.tcp://127.0.0.1:4840 -record '.\通信历史' read 'ns=2;s=Counter'
```

所有参数放在操作名前。多个 NodeID 使用独立参数，不按逗号拆分。用户名由 `-username` 提供，密码读取 `OPCUA_PASSWORD` 环境变量。安全参数为 `-security-policy`、`-security-mode`、`-cert`、`-key`、`-server-cert`；`-help` 列出全部参数。

退出码：0 全部成功，1 通讯/单项/记录/读回失败，2 参数错误或写入未获允许。JSON 中整数使用十进制字符串保留完整的 64 位精度。

## 构建与验证

```powershell
go test -tags ci -count=1 ./...
go vet -tags ci ./...
powershell -NoProfile -ExecutionPolicy Bypass -File build/windows.ps1 -Version 0.4.1 -CompilerRoot D:\tools\winlibs-gcc16\mingw64
```

Windows GUI 构建需要 x64 MinGW；运行程序无需编译环境，需要支持 OpenGL 的图形驱动。最终主程序 `OPCUAAIStudio.exe` 上限为 30,000,000 字节，写入图标及版本资源后超限则构建失败。产物位于 `dist/windows-gui/`，ZIP 包含 GUI、CLI、模拟器、说明和版本文件；校验与构建清单保存在 ZIP 同级目录。`-tags ci` 使用 Fyne 软件驱动进行自动化测试。

演示节点：Counter、Temperature、Enabled、Message、Samples、ReadOnly，均在 ns=2；ReadOnly 只读。首版支持单工作区、Endpoint 发现、浏览分页、节点读写和轮询；历史读取、方法调用、原生订阅不在本版范围。本机演示是协议测试服务，尚未通过现场服务器或 PLC 实机认证。

OPC UA 库固定为 [gopcua/opcua v0.9.1](https://github.com/gopcua/opcua/tree/v0.9.1)。结构和开发说明见 [开发说明](docs/development.md)，首版验证结果见 [验证记录](docs/validation/verification.md)。
