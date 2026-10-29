# Agent 公钥认证与升级

## Agent 版本识别

新版 Agent 在 `system_info.data.agentVersion` 上报构建版本，release 构建注入发布 tag，本地默认构建为 `dev`；设备上可通过 `swallow-agent -version` 查看同一版本值。Monitor 将版本保存在最新系统信息中，并在主机详情展示。未上报该字段的旧版（包括已有 RC1）显示“未知”，不会根据连接协议猜测版本。页面重新加载时读取最新已保存的版本。版本是设备自报信息，不替代公钥认证，也不构成二进制完整性证明。

## 发布检查与常规升级

主机管理页自动查询管理员接口 `GET /api/agent/releases`。Monitor 从固定的官方 GitHub Releases API 查询正式发布，过滤草稿、预发布和非法版本；内存缓存减少 GitHub 请求。该查询发生在 Monitor 服务端，Agent 无需访问 GitHub API。版本列表不等于本机安装状态，是否需要升级依据 Agent 最后上报的版本进行 SemVer 比较；`dev`、未知版本不能自动判断，离线设备的版本也可能不是当前磁盘上的版本。

常规升级仅面向已绑定公钥的受支持 Linux 主机：目标 release 含 `upgrade.sh` 及配套资产时，才生成 `bootstrap.sh --version TAG --mode upgrade` 命令；在目标主机手动执行，不由 Monitor 远程执行。标准 systemd 安装之外的部署和 Windows 需按其部署方式手动更新。升级不需要注册凭据，不修改配置或私钥；执行前仍应备份，并在执行后确认主机继续上报且版本已更新。服务启动成功不代表认证和采样已经成功。

旧 release（包括 `v0.1.0`）不因 Monitor 更新而自动获得新脚本。新增脚本需要随后续 Agent release 正式发布，不能覆盖旧 tag 资产。缺少升级资产时页面说明限制，不生成无法下载的命令。

新脚本支持可选 HTTPS GitHub Proxy 前缀，用于外层 bootstrap 及内部 release 资产下载；不向代理发送注册凭据。该设置不是 Monitor 查询 GitHub API 的网络代理。只使用自己信任的服务：二进制和校验清单同时由代理提供时，SHA-256 不是独立来源认证，代理仍处于下载信任边界。旧 release 不支持该参数时不能直接附加。

## Monitor 升级方法

Monitor 尚无独立版本号与发布产物（版本显示默认 `dev`，可在构建时注入），当前过渡阶段以 git 仓库主干为发布渠道。升级 Monitor 的方法：

1. 停止服务并备份 SQLite 数据库（主库与 WAL，或使用 SQLite 一致性备份）与 `config.yaml`。
2. 在部署目录 `git pull` 拉取当前最新代码（过渡版本即主干最新提交）。
3. 重新构建：源码部署执行前端构建与 `go build`；Docker 部署重新构建镜像并重建容器。
4. 重启服务，确认后台页 Monitor 版本显示、主机列表与实时上报恢复正常。

升级会保留数据库与配置；不要在未备份的情况下跨越多个未验证版本直接升级。Agent 的发布与 Monitor 的升级相互独立，但配套功能（如 Agent release 列表与升级命令）要求 Monitor 处于对应或更新版本。

## 自动注册与安装（默认）

管理员只填写昵称与标签创建待注册主机。界面仅在创建/刷新成功时显示一次
15 分钟有效的安装凭据、过期时间与安装命令；刷新页面不能找回旧明文。
界面自动获取 GitHub Releases 的正式版本列表，默认选择具有安装资产的最新正式版，也可从列表选择其他版本，无需手填版本号。生成的命令固定具体 tag，不使用可变的 latest 下载地址。缺少对应脚本、校验清单或二进制时不能生成对应命令；查询失败会明确提示，不视为已是最新版。

命令通过 HTTPS 从该 release 下载 bootstrap.sh 到 root 创建的私有临时文件，下载成功才执行，退出时清理。root 直接执行，非 root 使用 sudo；需 bash、curl，Agent bootstrap 还需要 sha256sum。下载及重定向仅允许 HTTPS，curl 的 stdin 为 /dev/null，凭据仅通过 stdin 交给 bootstrap，不使用 curl | bash，不把凭据放入进程参数。bootstrap 使用同一 --version 下载并校验安装脚本/二进制，--mode install 处理新安装。

一次执行自动生成/保留本地 Ed25519 私钥、注册绑定、写入配置并启动；无需手抄公钥或管理员再次批准。
私钥只留在 Agent 主机，限制文件权限，不能上传到 Monitor、日志或版本库。
注册凭据是短期秘密：持有者可以抢先绑定，这一风险在无审批流程中被接受。
不要分享命令、截图或日志；stdin 避免把凭据传给 Agent 命令行参数，但命令
仍可能留在 shell 历史、终端与剪贴板中。sudo 可能通过终端询问密码。

Agent 配置中的认证字段：

```yaml
privateKeyPath: /path/key.pem
reportUrl: wss://monitor.example.com/report
devMode: false
```

`reportUrl` 使用部署的实际 WSS 地址，Agent 验证服务端 TLS 证书与主机名。
Monitor 可由受信任反向代理终止 TLS；代理应转发 WebSocket Upgrade 和
`X-Agent-Public-Key`，后端监听端口不要暴露到不可信网络。
仅 loopback 本机开发允许 `ws`，且 Agent 必须显式开启 `devMode: true`；界面为本机 WS 安装命令附加 `--dev-mode`。
Monitor 的 `insecureDevelopment` 是另一个独立开关，会开放本机匿名管理，
不能代替 Agent 的 `devMode`，也不能作为生产配置。

## 管理 API

管理员调用 `POST /api/hosts`，提交 `{nickname,tags}`，成功返回 Host 对象加
`enrollmentToken`（64 位十六进制随机凭据）和 `enrollmentExpiresAt`（Unix 秒，15 分钟）。
公钥为空即待注册。管理员 `POST /api/hosts/{id}/enrollment` 可刷新待注册凭据，
返回同样结构；旧凭据立即失效，已绑定主机禁止刷新。列表/详情不能返回旧凭据明文，
服务端仅保存 SHA-256 摘要和期限；明文只在创建/刷新响应出现，不能用于长期上报。

Agent 使用一次性凭据及本地公钥完成自动绑定。消费凭据与绑定应原子完成，
过期、已使用或被刷新凭据无效；不能用注册凭据替换已绑定公钥。
若绑定响应丢失，保留原私钥，先用正常公钥挑战握手确认是否已绑定并恢复，
不要生成新钥或假设凭据可以重用。日常连接仅使用下面的公钥挑战认证。

旧手工公钥创建模式可在 API 保留，但不是默认界面流程：公钥为 32 字节
Ed25519 的规范标准 Base64（44 字符，末尾 `=`），不得重复绑定。
更新主机仅支持昵称和标签，不能换钥。

## 上报握手协议

1. Agent 连接 `/report`，携带 `X-Agent-Public-Key` 请求头（上述规范公钥）。
   Monitor 只接受已绑定的公钥；旧 `Token` 认证不再支持。
2. WebSocket 建立后，Monitor 发送
   `{"type":"auth_challenge","challenge":"<base64>"}`，其中 challenge 为
   新生成的 32 字节随机值的标准 Base64 字符串。
3. Agent 用 Ed25519 私钥签名以下 UTF-8 字节串（`\n` 表示单个 LF，末尾无换行）：

   ```text
   swallow-report-auth-v1\n<publicKey>\n<challenge>
   ```

   其中 publicKey 与 challenge 都是原始 Base64 文本，不是解码后的字节。
   Agent 返回 `{"type":"auth_response","signature":"<base64-signature>"}`。
4. Monitor 验证签名，并在生命周期锁内重新确认公钥绑定后登记在线连接。
   随后在锁外发送 `{"type":"auth_ok"}`，避免网络写入阻塞其他主机。
   Agent 收到 auth_ok 前不发送 system_info/system_usage。证明验证阶段固定
   10 秒期限、4096 字节消息上限；证明失败或超时不产生在线状态。
   auth_ok 写入另有 10 秒超时，失败会清理本次连接；此时可能短暂产生
   上下线事件。删除或新连接替换后，旧连接不能继续提交数据。

挑战签名证明持有私钥，不证明物理机器身份。公钥可公开，但复制私钥仍可
冒充该设备；当前 Hub 在同一公钥的新连接通过认证后替换旧连接。不要在
多台 Agent 上复用私钥。自动注册不提供硬件证明，也不允许原地换钥。

## 从 v0.0.2 Token 旧版接入（过渡方案）

旧版（v0.0.2 Token）Agent 的界面迁移渠道已随 Agent v0.1.2 移除：本界面不再生成迁移命令，Agent release 也不再附带迁移脚本。仍在 v0.0.2 的设备请 git 检出过渡版本 v0.1.1 的 SwallowAgent 源码（`git clone` 后 `git checkout v0.1.1`），按该版本的文档使用 `migrate-token-to-key.sh`，凭据用原主机记录上新生成的一次性凭据（即"新安装"命令中的凭据），手动完成迁移；之后即可使用常规升级入口升到最新版。

旧记录与数据库语义不变：升级前备份 SQLite 数据库、Monitor 配置，以及 Agent 的采集配置和私钥。数据库保留旧主机 ID、元数据和历史数据，旧记录的公钥保持未绑定，绝不把 Token 转为公钥；旧 Token 本身没有注册或上报能力。公钥为空的旧记录属于待注册主机，管理员必须在同一旧主机记录上刷新一次性凭据后由新版 Agent 自动绑定，保留该记录历史，不要删除重建。

已绑定公钥不可被替换。私钥泄露时应删除受影响记录并以新私钥重新接入。
**删除会关闭连接并删除该主机全部历史数据（原始、5 分钟和 1 小时层级），
不可撤销；删除重建不会继承历史，请先备份。**
