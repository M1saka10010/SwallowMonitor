# SwallowMonitor

SwallowMonitor 接收 SwallowAgent 上报的主机状态与资源用量，在浏览器中展示在线状态、实时变化和历史趋势。Monitor 将数据保存在 SQLite 中，Web 面板随 Go 二进制一同分发。被监控主机上运行的是独立的 Agent，不需要部署本仓库。

## 本机试用

需要 Go 1.25。仓库已包含构建好的面板；只运行 Monitor 时不需要安装 Node.js。若已有 `config.yaml`，请保留原文件，另建测试配置并在 `-c` 后指定其路径。

1. 在仓库根目录新建 `config.yaml`，写入以下**仅供本机开发**的配置：

   ```yaml
   listen: 127.0.0.1:8080
   insecureDevelopment: true
   dbPath: ./swallow.db
   ```

2. 运行 `go run . -c config.yaml`，浏览器打开 `http://127.0.0.1:8080`。地址必须与监听地址一致，不要换成 `localhost`。`GET /healthz` 返回 `ok` 表示进程可访问，不代表已有主机上报。
3. 初次启动时主机列表为空。要测试本机 Agent，按[设备接入说明](docs/AUTHENTICATION.md)创建主机并完成绑定。本机开发模式允许匿名管理，**不要通过端口转发或反向代理对外开放**。

## 生产部署

1. 从 [config.example.yaml](config.example.yaml) 复制生产配置到 `config.yaml`。保持 `insecureDevelopment: false`，填写 HTTPS `publicUrl`、GitHub OAuth 客户端信息和管理员白名单。GitHub 回调地址为 `publicUrl` 加 `/auth/github/callback`。监控视图默认公开；OAuth 保护的是管理操作，不是整个站点。
2. 使用仓库的 [Compose 配置](docker-compose.yml) 时，容器内使用 `listen: ":8080"`、`dbPath: /data/swallow.db`。在仓库根目录运行 `docker compose up -d --build`。SQLite 位于命名卷，宿主机仅在 `127.0.0.1:8080` 发布后端端口。
3. 按[生产安全清单](docs/SECURITY.md)配置 HTTPS 反向代理，保留外部 Host，并支持 WebSocket 与 SSE。用 `docker compose ps` 查看健康状态，再从公网域名验证登录、Agent 上报及面板实时更新。

**升级 Monitor：** 先停止服务并一致性备份数据库（包括未合并的 WAL）及 `config.yaml`，记录当前部署的提交。更新到选定的仓库提交后，Compose 部署重新执行 `docker compose up -d --build`；源码部署先在 `frontend/` 执行 `npm ci && npm run build`，再在根目录执行 `go build -o swallow-monitor .` 并重启服务。保留原数据卷，检查主机历史和实时上报；不要用清库重建代替迁移。Monitor 与 Agent 分别升级。

## 文档导航

| 我想做什么 | 阅读 |
| --- | --- |
| 安装、绑定或升级 Agent，从旧 Token 版本迁移 | [设备接入与公钥认证](docs/AUTHENTICATION.md) |
| 配置访问边界、反向代理、通知与凭据保护 | [生产安全清单](docs/SECURITY.md) |
| 对一台主机发起远程维护，处理断线或结果未知 | [远程维护任务](docs/MAINTENANCE.md) |
| 修改代码，定位配置、协议、存储或页面 | [仓库开发约定](agents.md) |
| 修改面板的布局、图表、主题或交互 | [Web 面板设计规范](docs/DESIGN.md) |
| 选择测试命令、验证迁移或准备发布 | [测试与验收](docs/TESTING.md) |
