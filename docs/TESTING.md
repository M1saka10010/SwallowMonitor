# 测试与验收

[文档首页](../README.md) · [仓库开发约定](../agents.md)

按改动范围选择检查。命令默认从仓库根目录执行；Go 使用 1.25，前端建议使用与 Docker 一致的 Node.js 24 和仓库提交的 npm 锁文件。

## 日常检查

| 改动 | 命令 | 验证什么 |
| --- | --- | --- |
| Go 后端 | `go test -count=1 ./...`，然后 `go build .` | 同时运行根包、业务包和跨模块测试；确认 Go 能嵌入现有面板 |
| WebSocket、SSE、通知或任务并发 | `go test -race -count=1 ./...` | 检测共享状态竞态；需支持 race 的平台、启用 cgo 和可用的 C 编译器 |
| Web 前端 | `cd frontend && npm ci && npm test && npm run build` | Vitest 交互与逻辑测试；TypeScript 类型检查；Vite 生成 `web/static/` |
| 镜像或部署 | `docker build .` | 从前端资源、Go 二进制到运行镜像的完整构建 |

快速定位可只跑一个包或场景，例如 `go test ./test -run '^TestTagAPIValidationAndCRUD$'`。查看覆盖率用 `go test ./... -coverprofile=coverage.out`，再用 `go tool cover -html=coverage.out`。局部测试不能代替合并前的全量回归。

### 可选集成测试

- `go test -tags=e2e ./test`：包含启动真实 Monitor 子进程的端到端测试，使用临时配置、临时数据库和本机端口。默认的 `go test ./...` 不包含此组。
- Linux 上验证旧版 Agent 迁移时，设置 `SWALLOW_AGENT_BINARY`（支持迁移的 Agent 可执行文件）或 `SWALLOW_AGENT_REPO`（对应源码目录），运行 `go test -tags=agentintegration ./test -run '^TestAgentTokenMigrationIntegration$'`。这是历史迁移检查，不代表当前 `/report` 接受 Token。

## 回归测试放在哪里

| 场景 | 位置与断言重点 |
| --- | --- |
| HTTP、WebSocket、SSE、静态资源等对外行为 | [test/](../test/)：用 `httptest` 搭建服务，断言状态码、响应字段与事件顺序；异步等待设超时。静态资源测试从 HTML 解析哈希资源名，不写死构建文件名。 |
| 配置与启动 | 根目录的 `main_test.go`、`security_config_test.go`：覆盖默认值、生产配置拒绝条件和仅回环开发模式。 |
| 会话、注册、挑战签名、限流与维护协议 | [server/](../server/) 的包内测试：既验证合法请求，也验证无权限、跨站、过期、重复或断线场景。认证语义见[设备接入](AUTHENTICATION.md)。 |
| 存储、迁移与任务去重 | [store/](../store/) 和跨模块测试：简单场景使用内存库，迁移用 `t.TempDir()` 构造旧库并重新打开；检查主机 ID、各层历史、绑定状态和任务记录。 |
| 页面、请求封装、图表与实时事件 | [frontend/src/](../frontend/src/) 下相邻的 `*.test.ts(x)`：断言用户可见状态、错误恢复、凭据过期和版本选择。用测试替身模拟网络、安装命令和维护操作。 |

修复缺陷时，先让回归用例在旧行为上失败。用例各自准备数据，用 `t.Cleanup` 释放资源；不要读写真实配置或数据库，不联网安装 Agent，不实际升级开发机。

## 发布前验收

1. 先构建前端，再运行 Go 全量测试和构建。`web/static/` 是随仓库提交的生成目录；源码和对应产物一起交付，不手改 JS/CSS。
2. 从 **Go 服务**访问面板，而不只检查 Vite：验证概览、主机详情、后台直达路由、SSE 刷新、主题切换；页面改动检查 375px、768px、1440px 布局及键盘操作。
3. 协议或权限变更要分别验证匿名读、管理员写和设备上报。维护任务的重连、去重与结果未知用例见[维护验收场景](MAINTENANCE.md)。记录实际执行的命令和未验证项。
