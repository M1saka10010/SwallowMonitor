# Agent 接入、认证与升级

[文档首页](../README.md) · [生产安全清单](SECURITY.md) · [远程维护](MAINTENANCE.md)

这里的凭据用于设备接入，与管理员的 GitHub OAuth 会话无关。设备只在**首次绑定**时使用安装凭据；日常上报和升级使用已绑定的密钥，不需要重新注册。

## 首次安装

1. 管理员在主机管理页填写昵称和标签，创建待注册主机。面板仅在创建或刷新凭据成功时显示安装命令；凭据有效期 **15 分钟**。刷新页面无法找回明文，未绑定前可重新生成，旧命令立即失效。
2. 从正式发布列表选择目标版本。默认选中具备安装资产的最新正式版；命令固定具体 tag，不使用 `latest`。缺少安装脚本、校验清单或二进制时不能生成命令。查询失败不等于已是最新版。
3. 在目标主机执行面板生成的命令。需要 bash、curl 和 sha256sum，非 root 用户还需 sudo。命令通过 HTTPS 将 `bootstrap.sh` 下载到私有临时文件，下载成功后才执行，退出时清理。下载不读取 stdin，凭据仅通过 stdin 传给安装程序，不放进进程参数。**整条命令仍是秘密**：终端历史、截图或剪贴板都可能泄露它。
4. 安装程序生成或保留本地 Ed25519 私钥，完成绑定、写入 Agent 配置并启动服务。回到主机页确认已绑定、持续上报并有新的采样；仅服务启动不能证明接入成功。

Agent 侧配置示例（不是 Monitor 的 `config.yaml`）：

```yaml
privateKeyPath: /path/key.pem
reportUrl: wss://monitor.example.com/report
devMode: false
```

生产使用 WSS 并验证服务端证书。只有回环地址允许明文 WS，且 Agent 必须启用 `devMode`。它和 Monitor 的 `insecureDevelopment` 是两个独立开关。私钥只留在当前设备，限制文件权限；复制私钥的机器也能冒充该设备。

## 已绑定设备：检查版本与升级

- 详情页显示 `system_info.data.agentVersion` 最近一次上报值。发布构建使用 tag，本地构建默认 `dev`，缺失显示“未知”；也可在设备上运行 `swallow-agent -version`。版本是自报数据，不是完整性证明。
- 管理页经 `GET /api/agent/releases` 获取 Monitor 服务端缓存的官方 GitHub 正式发布列表，排除草稿、预发布和非法版本。版本可比较时按 SemVer 判断；缓存过期、检查失败、`dev` 或未知版本不能作为“已是最新”的依据。
- **手动升级**：对受支持的已绑定 Linux 主机选择具有对应升级资产的 release，在目标主机执行面板生成的升级命令。升级不使用注册凭据，不换私钥。非标准 systemd 布局、Windows 或缺少资产时按 Agent 的部署方式手动更新。不要假设旧 tag 会补发新脚本。
- **远程升级**：仅在 Agent 已显式启用维护能力时，才可从 Monitor 下发固定任务；见[远程维护](MAINTENANCE.md)。两种方式都应先备份，完成后验证版本与上报。
- 支持的 release 可选 HTTPS GitHub 下载代理，用于 bootstrap 和后续资产下载，不是 Monitor 查询发布 API 的代理。凭据不发送给下载代理；代理同时提供二进制与 SHA-256 清单时，校验不能独立证明发布者身份。

## 协议参考

### 一次性绑定

| 调用 | 语义 |
| --- | --- |
| 管理员 `POST /api/hosts`，提交 `{nickname,tags}` | 返回 Host、`enrollmentToken`（64 个小写十六进制字符）和 `enrollmentExpiresAt`（Unix 秒）。空公钥表示待注册。 |
| 管理员 `POST /api/hosts/{id}/enrollment` | 仅待注册主机可刷新；作废旧凭据。数据库仅保存凭据摘要（SHA-256）与期限，列表和详情不返回明文。 |
| Agent `POST /enroll` | 提交 `token`、`publicKey`、`signature`。服务端验证签名后原子消费凭据并绑定公钥；已使用、过期或被刷新的凭据无效。 |

公钥是 32 字节 Ed25519 公钥的规范标准 Base64（44 字符，末尾 `=`）。手工指定公钥创建主机的 API 不是默认安装流程；不能重复绑定，更新昵称或标签不能换钥。自动注册不需要二次批准，持有有效凭据者可能抢先绑定。若绑定响应丢失，**保留原私钥，先走正常上报握手**，不要立即生成新钥。

### 每次上报连接

1. Agent 连接 `/report`，在 `X-Agent-Public-Key` 中发送已绑定公钥。旧 `Token` 不被接受。
2. Monitor 发送 `{"type":"auth_challenge","challenge":"<base64>"}`，challenge 为新生成的 32 字节随机数的标准 Base64。
3. Agent 用私钥签名 UTF-8 字节串 `swallow-report-auth-v1\n<publicKey>\n<challenge>`。`\n` 表示一个 LF，末尾没有换行；公钥和 challenge 均使用原始 Base64 文本。返回 `{"type":"auth_response","signature":"<base64-signature>"}`。
4. Monitor 验签并再次核对绑定后登记在线，随后发送 `{"type":"auth_ok"}`。证明阶段限 10 秒、4096 字节；失败不登记在线。`auth_ok` 写入另有 10 秒期限，写失败会清理本连接，可能产生短暂上下线事件。Agent **收到 `auth_ok` 后**才发送系统信息和采样数据。相同公钥的新认证连接替换旧连接，被替换的连接不得继续写入。

## 旧 Token 主机与私钥丢失

升级前备份 Monitor 配置和 SQLite 数据，以及 Agent 配置和现有私钥。旧库迁移保留主机 ID、元数据和历史，不把 Token 转为公钥。要保留历史，在**同一条待注册记录**上刷新一次性凭据后绑定，不要删除重建。

仍运行 v0.0.2 Token Agent 的设备：自 Agent v0.1.2 起，面板与 release 不再提供旧迁移入口。手动检出 **SwallowAgent 的 `v0.1.1` 过渡版本**，按该版本文档使用 `migrate-token-to-key.sh` 和原记录的新凭据迁移，再走常规升级。

已绑定主机不能原地换钥。私钥泄露或无法恢复时，先隔离并备份，再以新密钥重新接入。**删除旧记录会断开连接并永久删除其原始、5 分钟和 1 小时历史；新记录不会继承这些数据。**
