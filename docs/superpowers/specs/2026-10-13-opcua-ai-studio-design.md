# OPC UA AI Studio 首版设计

用户要求参考 `s7-ai-studio` 新建独立项目。默认采用同级目录、Go + Fyne 中文桌面工具，复用窗口适配、主题、图标、SQLite 记录和 AI 诊断组件。原项目工作区不修改。

## 功能与边界

- 三个入口：`opcua-ai` 桌面、`opcua-cli` 命令行、`opcua-sim` 本机演示。
- OPC TCP Endpoint 发现；按明确选择的安全策略与模式连接，不自动降级。
- 匿名及用户名认证；加密连接提供客户端证书、私钥和固定信任的服务器证书。
- NodeID 浏览（含分页）、多节点读取、标量与一维数组写入、轮询。
- 支持 Boolean、SByte/Byte、Int16/UInt16、Int32/UInt32、Int64/UInt64、Float/Double、String、DateTime、ByteString；读取 Auto 自动识别类型。
- 写入默认禁用，GUI 逐次确认 Endpoint/NodeID/类型/值，并绑定连接代次；收到成功状态后独立读回。未知结果不自动重试。
- 本机演示绑定回环地址，提供 Counter=42、Temperature=23.5、Enabled=true、Message="Hello OPC UA" 和数组 Samples。
- 通讯事件记录到独立 `%AppData%\OPCUAAIStudio\records`。记录服务级请求和结果，不声称记录加密连接的原始线缆字节。
- AI 仅由用户点击后发送当前可预览的脱敏证据，沿用 DeepSeek 客户端；不能执行设备操作。
- 首版不实现历史读取、方法调用、自动网络扫描或原生订阅；连续监测采用轮询。

## 架构

`internal/device` 保存 NodeID、类型化值、质量和时间戳；`internal/opcua` 包装固定版本 SDK；`internal/service` 管理连接代次、取消、轮询、记录和演示；`internal/ui` 与 `internal/cli` 共享通讯层。`platform`、`assets/icon`、`internal/recorder` 和 `internal/ai` 由参考项目复用并改为独立标识。

## 失败处理与验证

无效 Endpoint、NodeID、类型、数值及安全配置在连接/写入前拒绝。节点错误保留各项状态；连接中断停止轮询并要求重新连接。64 位整数按十进制字符串序列化，避免 JSON 精度损失。密码及 API Key 不写日志或普通首选项。

验证覆盖 NodeID 大小写和分隔符、Unicode 字符串、64 位整数、部分失败、服务端读写、浏览分页、只读门控、旧确认失效、轮询停止、演示退出及记录排空。运行全部测试、静态检查、CLI 往返测试和 Windows GUI 构建与截图验证。
