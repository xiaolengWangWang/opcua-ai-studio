# OPC UA AI Studio 实施计划

依据：../specs/2026-10-13-opcua-ai-studio-design.md。在独立同级项目中顺序实施。

1. 复用通用包和独立应用标识；新增 `device.Address{NodeID, Type, Count}`、类型值转换及测试。NodeID 不转大写；整数不经浮点数。
2. 实现 `opcua.Dial`、`Endpoints`、`Read`、`Write`、`Browse`；固定 SDK v0.9.1；显式安全选择、服务器证书比对和每节点状态保留。以真实回环服务器测试。
3. 实现 `simulator.Start(ctx, endpoint)` 与演示节点；复用 Controller 的取消和代次管理，增加 Browse，改写读回比较和本机演示。
4. 实现 CLI 的 endpoints/browse/read/write；共享连接选项，支持 JSON、记录与写入门控；以回环节点验证完整流程。
5. 实现中文 Fyne 桌面连接、安全参数、节点浏览、读写、轮询、记录/日志及 AI 证据预览；独立首选项，密码不持久化。
6. 配置 Windows 打包、CI 和 README；运行 `go test ./...`、`go vet ./...`、CLI 往返及 Windows 构建；GUI 启动截屏。将验证证据保存到 docs/validation。

重点检查：NodeID 中逗号与大小写、UInt64 最大值、服务端部分错误、UNKNOWN 写入不重发、连接变化后旧写入确认失效。
