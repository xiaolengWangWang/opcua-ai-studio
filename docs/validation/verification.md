# 0.1.0 验证记录

验证日期：2026-10-13。环境：Windows x64、Go 1.26.6、MinGW GCC 16、gopcua/opcua v0.9.1。项目位于 `D:\workspace\opcua-ai-studio`，为独立本地 Git 仓库；首次验证时尚未提交或设置远程仓库。

## 自动化检查

以下命令最终均退出 0：

```powershell
go test -tags ci -p 1 -count=1 -timeout 3m ./...
go vet -tags ci ./...

$env:CGO_ENABLED = '1'
$env:CC = 'D:\tools\winlibs-gcc16\mingw64\bin\gcc.exe'
$env:PATH = 'D:\tools\winlibs-gcc16\mingw64\bin;' + $env:PATH
$env:GOTMPDIR = Join-Path (Get-Location).Path '.test-tmp'
go test -race -tags ci -p 1 -count=1 -timeout 3m ./...
```

测试覆盖所有包，包括 NodeID 解析、64 位整数精度、各类标量及数组、CLI 参数与写入门控、连接生命周期、过期写入确认、独立读回、轮询停止、SQLite 轮转与关闭、首选项及 AI 证据处理。

本机协议测试验证了 235 个节点的浏览分页、逐项读取失败、只读节点拒绝，以及 Basic256Sha256 / SignAndEncrypt 加密连接和不匹配服务器证书的拒绝。写入已生效但确认丢失、服务端返回 BadTimeout 的情况均返回 `UNKNOWN`；不会自动重发写入。

## Windows 发布包

`build/windows.ps1` 成功生成 GUI、CLI、模拟器及 ZIP。ZIP 内含三个程序、README.txt 和 VERSION.txt。

| 产物 | SHA-256 |
| --- | --- |
| OPCUAAIStudio-0.1.0-Windows-x64.zip | `acfc8face700414cf478708635d655fe39f8c62e4e01aa12d2864c002737cf00` |
| OPCUAAIStudio.exe | `37cd918390a29a28e93ba48ddb3575fdaa214e2b29cfc554d68d3fd33340ef06` |

ZIP 大小为 15,525,071 字节。完整产物、校验文件与构建清单位于 `dist/windows-gui/`。

使用此发布包的真实 Windows GUI 执行本机演示验证，程序正常退出并确认：

- Counter 初始值 42，写入 123 后独立读回为 123，写入状态为 `CONFIRMED`。
- 浏览得到 6 个演示节点，并读取整数、浮点、布尔、字符串和数组。
- 保存 10 条通讯事件，正常关闭后记录排空、会话结束、演示端口释放。
- GUI 截图中的中文字体、节点表格和读回结果正常显示。

结果见 [smoke.json](release-smoke/smoke.json) 和 [原生 GUI 截图](release-smoke/native-gui.png)。SQLite 文件为本地测试产物，已排除在 Git 跟踪范围外。

发布包的 CLI 另行连接本机模拟器，完成节点浏览、Counter / Samples 读取、中文字符串“供水正常”写入读回，以及 Int32 数组 `[4,5,6]` 写入读回；命令均成功。读取样例见 [cli-read.json](cli-read.json)。

## 2026-10-14 续接复核

再次运行 `go test -tags ci -p 1 -count=1 -timeout 3m ./...` 和 `go vet -tags ci ./...`，均退出 0。

核对现有 Windows ZIP 的大小、SHA-256、GUI SHA-256 和包内五个文件，均与构建清单及解压目录一致；发布包未重新构建，校验值沿用上表。

再次运行发布包中的 `OPCUAAIStudio.exe -smoke-test docs/validation/continuation-smoke-20261014`，程序退出 0。Counter 初值 42，写入 123 后独立读回一致，状态为 `CONFIRMED`；浏览得到 6 个节点，保存 10 条通讯记录，正常关闭后端口释放。查看原生 GUI 截图，中文和结果表格显示正常。

本次证据见 [smoke.json](continuation-smoke-20261014/smoke.json) 和 [原生 GUI 截图](continuation-smoke-20261014/native-gui.png)。首版检查与交付已完成；本次未新增功能或进行现场连接测试。

## 验证范围

以上结果来自本机协议服务、模拟器和本地 HTTP 测试服务器。尚未连接现场 OPC UA 服务器或 PLC；AI 客户端经过本地测试，未调用线上 DeepSeek 服务。现场证书、用户权限、服务器支持的数据类型与图形驱动需在实际环境确认。
