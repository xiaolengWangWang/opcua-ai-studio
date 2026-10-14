# 开发说明

模块名：`opcua-ai-studio`，初始版本 `0.1.0`。这是独立项目，代码仓库为 [xiaolengWangWang/opcua-ai-studio](https://github.com/xiaolengWangWang/opcua-ai-studio)，默认分支为 `main`；参考项目中的未提交修改没有被改动。

| 目录 | 职责 |
| --- | --- |
| cmd/opcua-ai | Fyne 桌面入口及本机 smoke 验证 |
| cmd/opcua-cli | endpoints / browse / read / write |
| cmd/opcua-sim | 只绑定回环地址的演示服务 |
| internal/device | NodeID、精确类型值、状态及时间戳 |
| internal/opcua | SDK 适配、端点选择、证书比对、浏览分页 |
| internal/service | 连接生命周期、写入代次、读回、轮询、记录 |
| internal/ui | 中文界面、首选项、AI 证据预览 |
| internal/recorder | 复用 SQLite 历史记录、压缩、轮转及迁移能力 |
| internal/ai | 复用 DeepSeek 诊断、证据校验和 Windows 加密凭据工具 |
| platform / assets/icon | 复用桌面字体、窗口适配和图标 |

保留 recorder 对旧数据库协议字段的读取能力，不提供旧协议通讯驱动。服务级记录不伪装为原始抓包。

`go test -tags ci ./...` 覆盖全部包。Windows 本机构建使用 build/windows.ps1；GUI 支持 `-smoke-test <目录>`，自动连接本机演示、确认 Counter 从 42 写为 123、浏览/读多个类型、保存截图、验证记录排空和端口释放，最后退出。

AI 单元测试使用本地 HTTP 测试服务器，不消耗线上模型用量。安全测试使用本机自签名证书验证 Basic256Sha256 / SignAndEncrypt 和不匹配服务器证书的拒绝；尚未验证现场服务器。安全策略与证书选项在 SDK 边界处理，未匹配端点即返回错误。
