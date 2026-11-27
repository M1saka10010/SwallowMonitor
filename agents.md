# SwallowMonitor 开发约定

[文档首页](README.md) · [测试与验收](docs/TESTING.md) · [Web 面板设计规范](docs/DESIGN.md)

本文件适用于本仓库的开发者与编码助手。这里实现 Monitor 服务端和 Web 面板；设备端 Agent 是另一个项目。修改协议时要考虑两端的兼容性，不能只更新面板文案。

## 从哪里改

| 改动 | 主要位置 | 同步检查 |
| --- | --- | --- |
| 启动、配置、部署 | `main.go`、`model/configModel.go` | 配置校验、`config.example.yaml`、Compose 挂载与部署说明 |
| HTTP API、WebSocket 上报、SSE | `server/`；消息结构在 `model/message.go` | `frontend/src/api/` 的请求和类型、`frontend/src/live/` 的事件消费、Agent 协议 |
| SQLite 表结构、查询与清理 | `store/` | 新库初始化、旧库升级、历史数据与身份绑定状态 |
| 页面与交互 | `frontend/src/pages/`、`frontend/src/components/` | 相邻测试、主题、键盘交互；重建 `web/static/`，不要手改生成资源 |

## 本地开发与交付

1. 先按 [README](README.md) 启动 Go 服务。修改面板时可在第二个终端运行 `cd frontend && npm ci && npm run dev`；代理目标见 `frontend/vite.config.ts`。涉及 Host、同源校验或登录回调时，从与 `publicUrl` 一致的 Go 服务地址验证，不把 Vite 页面当作生产部署。
2. 保持改动集中，Go 文件用 `gofmt`，前端复用现有类型和请求封装。修复缺陷时先补能复现原问题的测试，不为通过测试放宽断言。
3. 对照[测试矩阵](docs/TESTING.md)选择检查。面板源码与构建产物一同交付。说明行为变化、实际执行的命令与未验证项；协议和数据库变更另说明兼容性、升级与备份要求。不要提交本地配置、凭据或数据库。

## 必须保持的行为

- **权限在服务端检查。** 管理员会话、一次性注册凭据和设备长期密钥不能互换。设备通过挑战签名验证后才登记在线，收到 `auth_ok` 后才上报；不要恢复旧 Token 认证。见[认证协议](docs/AUTHENTICATION.md)和[安全清单](docs/SECURITY.md)。
- **远程维护只接受固定操作。** 同一主机不能并发执行维护任务；断线不等于失败，重连沿用原任务 ID。测试使用模拟执行器，不能实际升级开发机。见[维护规则](docs/MAINTENANCE.md)。
- **专题只保留一个事实来源。** 新增运行入口更新 README；变更协议、测试方式或 UI 约束时更新对应专题，用相对链接引用，不在其他文档复制整段流程。
