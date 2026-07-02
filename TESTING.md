# SwallowMonitor 自动化测试文档

本文档只描述 SwallowMonitor 的自动化测试体系、运行命令、覆盖范围和新增测试规范。

## 1. 测试目标

- 自动验证 SQLite 数据存储层的 CRUD、迁移和关联关系。
- 自动验证 HTTP API 的状态码、响应体、参数校验和鉴权行为。
- 自动验证 WebSocket 上报连接触发主机在线/离线状态变更。
- 自动验证通知规则匹配和通知分发逻辑。
- 通过持续运行测试降低核心功能回归风险。

## 2. 测试目录

当前自动化测试集中在 `test/` 目录：

| 测试文件 | 覆盖模块 | 自动化验证点 |
| --- | --- | --- |
| `test/settings_test.go` | 站点设置 | 默认值、更新、API GET/PATCH、参数校验、鉴权 |
| `test/tag_test.go` | 标签与主机关联 | 标签 CRUD、主机标签过滤、接口校验、鉴权、旧库迁移 |
| `test/notification_test.go` | 通知规则 | 规则 CRUD、匹配逻辑、接口校验、鉴权、在线/离线通知分发 |
| `test/host_usage_report_test.go` | 主机、Usage、上报链路 | 主机 CRUD、Token 查询、Usage 查询/清理、WebSocket 上报 |
| `test/auth_events_static_test.go` | 鉴权状态、SSE、静态资源 | `/api/me`、登录/登出状态、`/events`、嵌入式静态资源 |

约定：所有 Go 自动化测试文件都放在 `test/` 目录中，不在根目录或业务包目录新增 `_test.go`。

## 3. 测试环境

- Go：项目 `go.mod` 声明为 `go 1.25.0`。
- 数据库：自动化测试默认使用 SQLite 内存库 `:memory:`。
- 文件数据库：迁移类测试使用 `t.TempDir()` 创建临时数据库文件。
- 配置文件：测试不依赖本地 `config.yaml`，测试用例直接构造配置对象。

## 4. 运行测试

### 4.1 运行全部自动化测试

```powershell
go test ./...
```

预期结果：命令退出码为 `0`，所有包测试通过。

### 4.2 运行测试包

```powershell
go test ./test
```

### 4.3 运行指定测试用例

```powershell
go test ./test -run TestTagAPIValidationAndCRUD
```

### 4.4 查看详细输出

```powershell
go test -v ./...
```

### 4.5 关闭测试缓存

```powershell
go test -count=1 ./...
```

适用于确认测试结果来自本次运行，而不是 Go 测试缓存。

### 4.6 并发与竞态检测

```powershell
go test -race ./...
```

适用于验证 WebSocket、通知分发、状态更新等并发路径。

## 5. 覆盖率

### 5.1 输出整体覆盖率

```powershell
go test ./... -cover
```

### 5.2 生成覆盖率文件

```powershell
go test ./... -coverprofile=coverage.out
```

### 5.3 查看 HTML 覆盖率报告

```powershell
go tool cover -html=coverage.out
```

### 5.4 覆盖率环境说明

如果本地执行覆盖率命令出现以下错误：

```text
go: no such tool "covdata"
```

说明当前 Go 工具链缺少覆盖率组件，需要修复或重新安装 Go 工具链后再生成覆盖率。

### 5.5 建议关注的覆盖空白

- GitHub OAuth 完整 callback 成功路径和失败路径。
- `main` 包中未导出的配置解析默认值和后台清理循环。
- `web/static` 前端交互逻辑。
- Docker 构建流程。

## 6. 当前自动化测试用例

### 6.1 站点设置

- `TestSiteSettingsDefaultsAndUpdate`
- `TestSettingsAPIGetAndPatch`
- `TestSettingsAPIValidation`
- `TestSettingsAPIPatchRequiresAuth`

覆盖内容：默认站点名称、站点描述更新、接口读写、站点名称空值和长度校验、开启 OAuth 后写接口鉴权。

### 6.2 标签与主机关联

- `TestTagStoreCRUDAndHostAssociation`
- `TestTagAPIValidationAndCRUD`
- `TestTagAPIRequiresAuth`
- `TestHostAPIUsesExistingTagsOnly`
- `TestOpenMigratesLegacyHostTagsWithoutDeadlock`

覆盖内容：标签创建、重命名、删除、主机标签关联、接口状态码、空标签校验、鉴权、旧版本 `hosts.tags` 数据迁移。

### 6.3 通知规则

- `TestNotificationRuleStoreCRUDAndMatching`
- `TestNotificationAPIValidationAndCRUD`
- `TestNotificationAPIRequiresAuth`
- `TestNotificationDispatchOnStatusChange`

覆盖内容：通知规则创建、更新、删除、按标签匹配、上线/离线开关、URL 校验、鉴权、WebSocket 连接变化触发通知。

### 6.4 主机、Usage 与上报链路

- `TestHostStoreCRUDTokenLookupInfoTouchAndDelete`
- `TestHostAPICRUDVisibilityValidationAndAuth`
- `TestHostAPITokenHiddenForAnonymousWhenOAuthEnabled`
- `TestUsageStoreLatestQuerySamplingAndPrune`
- `TestUsageAPIQueryParametersAndRangeClamp`
- `TestReportWebSocketAuthInfoUsageAndBadMessages`

覆盖内容：主机创建、更新、删除、Token 查询、匿名隐藏 Token、主机鉴权、Usage 插入、最新数据、区间查询、降采样、历史清理、Usage API 参数反转、WebSocket Token 鉴权、`system_info` 和 `system_usage` 上报。

### 6.5 鉴权状态、SSE 与静态资源

- `TestMeLoginLogoutAuthStates`
- `TestEventsStreamsRetryStatusAndUsageData`
- `TestStaticHandlerServesEmbeddedFiles`

覆盖内容：未开启 OAuth 时的登录状态、开启 OAuth 后的匿名状态、登出清理 Cookie、SSE retry/status/usage 事件流、嵌入式 HTML/JS/CSS 静态资源和缺失文件 404。

## 7. 新增自动化测试规范

### 7.1 测试命名

所有测试文件必须放在 `test/` 目录中，包名统一使用：

```go
package test
```

如需测试未导出函数，优先改为通过公开 API、HTTP 路由、WebSocket 或 Store 公开方法验证行为；不要为了测试把 `_test.go` 放回业务包目录。

测试函数使用 Go 标准命名：

```go
func Test功能场景(t *testing.T) {
    // ...
}
```

推荐命名示例：

- `TestHostAPIValidationAndCRUD`
- `TestNotificationDispatchOnStatusChange`
- `TestOpenMigratesLegacyHostTagsWithoutDeadlock`

### 7.2 测试数据隔离

- 优先使用 `store.Open(":memory:")` 创建隔离数据库。
- 文件数据库测试必须使用 `t.TempDir()`。
- 不要读写仓库根目录下的 `swallow.db`。
- 每个测试独立准备数据，不依赖测试执行顺序。
- 测试结束时使用 `t.Cleanup` 释放资源。

### 7.3 Store 层测试

- 直接调用 `store` 包方法验证数据写入、查询、更新、删除。
- 同时验证正常路径和边界路径。
- 涉及迁移时，先构造旧结构数据库，再调用 `store.Open` 验证迁移结果。

### 7.4 HTTP API 测试

- 使用 `httptest.NewRequest` 构造请求。
- 使用 `httptest.NewRecorder` 捕获响应。
- 先断言 HTTP 状态码，再解析响应体。
- 写接口需要覆盖成功、非法参数、未授权三类场景。
- JSON 响应使用 `json.NewDecoder` 解码后断言结构字段。

### 7.5 WebSocket 测试

- 使用 `httptest.NewServer` 启动测试服务。
- 使用 `websocket.DefaultDialer.Dial` 建立连接。
- 异步通知和状态变化必须设置超时时间。
- 连接关闭后需要验证离线相关行为。

### 7.6 鉴权测试

开启鉴权的测试可构造：

```go
&model.Config{GitHub: model.GitHubConfig{ClientID: "client"}}
```

需要验证写接口在没有有效会话时返回 `401 Unauthorized`。

### 7.7 断言风格

- 初始化失败使用 `t.Fatalf`。
- 辅助函数必须调用 `t.Helper()`。
- 失败信息应同时包含实际值和期望值。
- 不要吞掉错误，所有返回的 `error` 都需要断言。

## 8. 建议补充的自动化测试

后续建议优先补齐以下测试：

| 优先级 | 建议测试 | 说明 |
| --- | --- | --- |
| 高 | OAuth 回调 | 使用 httptest 模拟 OAuth 成功和失败路径 |
| 中 | 配置解析 | 当前要求测试集中在 `test/`，`main.loadConfig` 未导出，需通过公开入口或重构后覆盖 |
| 中 | 离线超时判定 | 覆盖无活动连接但 `last_seen` 未超时/已超时的展示状态 |
| 低 | 前端 JS 单元测试 | 覆盖纯函数、请求封装、DOM 更新逻辑 |

## 9. CI 建议

建议在 CI 中至少执行：

```powershell
go test -count=1 ./...
```

如果 CI 环境支持竞态检测，建议增加：

```powershell
go test -race -count=1 ./...
```

注意：`-race` 需要启用 cgo；如果输出 `go: -race requires cgo`，需要设置可用的 C 编译环境并启用 `CGO_ENABLED=1`。

如果需要覆盖率产物，建议增加：

```powershell
go test -count=1 ./... -coverprofile=coverage.out
```

注意：覆盖率命令依赖完整 Go 工具链；如果缺少 `covdata`，需先修复本地 Go 安装。

## 10. 回归测试流程

修复缺陷时按以下流程补自动化测试：

1. 先新增能复现问题的失败测试。
2. 再修改业务代码让测试通过。
3. 运行 `go test -count=1 ./...` 确认无回归。
4. 在提交说明或变更说明中写明新增的测试用例。

## 11. 发布前自动化检查

发布或合并前建议执行：

```powershell
go test -count=1 ./...
go test -count=1 ./... -cover
```

涉及并发、WebSocket、通知分发时额外执行：

```powershell
go test -race -count=1 ./...
```
