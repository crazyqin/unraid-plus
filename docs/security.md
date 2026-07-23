
---
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '2aa45dd6-df9c-469e-9fb0-8510636e1862'
  PropagateID: '2aa45dd6-df9c-469e-9fb0-8510636e1862'
  ReservedCode1: 'b92ff0ee-18f6-4dcd-bcd7-5f16f731fd1b'
  ReservedCode2: 'b92ff0ee-18f6-4dcd-bcd7-5f16f731fd1b'
---

# 安全模型（Security）

unraid-plus 是一个以 root 权限操作 NAS 的代理，安全是头等大事。本文档说明威胁模型、
凭据处理、主机密钥校验、文件沙箱与加固建议，**部署前请务必读完第 6 节**。

## 1. 威胁模型

unraid-plus 在以下假设下设计：

| 资产 | 价值 | 谁想拿到 |
| --- | --- | --- |
| Unraid root 凭据 | 极高（= 完全控制 NAS） | 任何能监听 / 攻陷链路的人 |
| 浏览器 ↔ 后端 流量 | 高（含密码、终端字节流） | 局域网嗅探者、公网中间人 |
| 后端 ↔ Unraid 流量 | 中（局域网内 SSH） | 局域网内的恶意主机 |
| 主机密钥指纹 | 中（防 MITM） | 局域网 ARP 欺骗者 |

**不被覆盖**的威胁：用户本机被植入木马、Unraid 自身被攻陷、后端主机 root 被
攻击者拿到——这些超出 unraid-plus 能防御的范畴。

## 2. 凭据处理

### 2.1 root 密码（默认模式）

- **浏览器 → 后端**：用户在引导向导里输入一次，走 POST `/api/connect`。
  **强烈建议这条链路必须是 HTTPS**（参见 §4）。
- **后端持有**：成功连接后密码保存在 `ssh.Pool` 的 `managedConn.password`，
  **仅存在于内存**。重启后端即丢失，需要重新输入。
- **不落盘**：默认不写任何文件。`UNRAIDPP_DATA_DIR` 里看不到它。
- **重连**：连接断开时 pool 会用内存里的密码自动重试；若重连失败密码仍保留至进程
  退出或显式 `/api/disconnect`。

> 我们刻意没有「记住密码」的勾选框。这是有意的安全性权衡：用户应该改用密钥模式
> 而不是把密码存在磁盘上。

### 2.2 密钥模式（推荐）

`Settings` 页面提供「切换到密钥模式」按钮，行为：

1. 后端使用 `crypto/rand` 现场生成一对 ED25519 密钥（`id_ed25519` /
   `id_ed25519.pub`）。
2. 公钥通过当前 SSH 会话 append 到 Unraid 的
   `/boot/config/ssh/authorized_keys`（这是 Unraid 持久化 SSH 配置的位置）。
3. 私钥写入 `UNRAIDPP_DATA_DIR/id_ed25519`，权限 `0600`，owner 为运行进程的系统
   用户。
4. pool 切换为密钥认证，**内存中的密码字段被清零**。
5. 后续连接不再需要 root 密码。重启后端会自动读 `id_ed25519` 重连。

这是与 Easy Unraid 的核心差异之一：用户**不需要**先在桌面生成密钥、再 scp 上传、
再改 authorized_keys——unraid-plus 一键搞定，且整个过程是 SSH 通道内完成的。

### 2.3 会话密钥

后端用 `UNRAIDPP_SESSION_KEY` 给浏览器 session cookie 签名：

- **未设置**：每次启动生成 32 字节随机 key（`crypto/rand`）。**重启后所有浏览器会话
  失效**，需要重新走引导。这是默认行为，安全而简单。
- **显式设置**：固定 key 适合容器化部署，重启会话不丢。要求 key 长度 ≥ 16 字节，
  建议用 `openssl rand -base64 32` 生成后通过 secret 注入，**不要写进 compose
  文件提交到仓库**。

## 3. 主机密钥校验（TOFU）

Unraid 默认 SSH 配置没有预设已知主机，第一连接时如果直接信任任何密钥，会被 MITM。
unraid-plus 采用 **Trust On First Use（TOFU）**：

1. **首次连接**：后端记下服务器返回的 host key，计算 SHA-256 指纹（
   `hostkey.go` 里的 `FingerprintOf`），在引导页面向用户展示，用户点「确认」后入库。
2. **后续连接**：通过 `knownHosts.callback` 与记录的密钥对比。不匹配则拒绝连接、
   返回指纹变化告警，避免被悄悄替换。
3. **指纹变更**：可能原因——重装系统、改 SSH 端口、真的被攻击。后端不擅自信任，
   用户需在 Settings 里手动清掉才能继续（防 MITM 滑坡）。

缓存存放位置：
- 默认：内存，进程重启即失效（首次连接需要重新确认）。
- 启用持久化：`UNRAIDPP_DATA_DIR/known_hosts`，格式兼容 OpenSSH，便于人工核对 /
   清理。

## 4. 浏览器 ↔ 后端 链路

这是 unraid-plus 安全性最薄弱的一环，**默认监听 `:8080` 是明文 HTTP**。
必须满足以下任一条件：

1. 走反代套 HTTPS（Caddy / Nginx / Traefik，见 [deployment.md
   §6](deployment.md)）。
2. 部署在 Tailscale / WireGuard 隧道内，浏览器通过隧道访问。
3. 仅本机 `127.0.0.1` 访问（开发场景）。

**绝对不要**把 `:8080` 直接暴露到公网。攻击者不仅能截获 root 密码，还能通过
WebSocket 终端直接拿到 root shell。

## 5. 文件操作沙箱

SFTP 文件管理接口（`/api/files`、`/api/files/delete`）对路径做了白名单校验：

- 允许前缀：`/mnt`、`/root`、`/tmp`。
- 其他路径（例如 `/etc`、`/boot`、`/usr`）一律拒绝，即使 SSH 用户是 root。

这能把「手滑删除系统文件」类事故挡住。

要扩展允许范围（例如管理 `/boot/config/plugins`），在
`server/internal/api/handler/files.go` 的 `allowedRoots` 加前缀即可，但请确保你
理解风险——`/boot` 在 Unraid 上是启动盘的持久化配置区，误删可能让 NAS 无法启动。

## 6. 部署前加固清单

部署到生产前请逐条核对：

- [ ] **链路加密**：unraid-plus 前面套了 HTTPS 反代，或部署在 VPN/Tailscale 内。
- [ ] **端口不暴露公网**：`docker compose` 的 `ports: 8080:8080` 改成
      `127.0.0.1:8080:8080`，或加防火墙规则。
- [ ] **切到密钥模式**：首次连接成功后立刻在 Settings 里启用，清空内存密码。
- [ ] **固定 session key**：`UNRAIDPP_SESSION_KEY` 通过 secret 注入，重启不失效。
- [ ] **非 root 运行**：容器 / systemd 以专用 unraid-plus 用户跑（Dockerfile 已内置
      `USER app`，systemd 单元见 [deployment.md §4.2](deployment.md)）。
- [ ] **最小权限 SSH**：在 Unraid 的 `/etc/ssh/sshd_config` 里把 unraid-plus 用的
      密钥限制为 `restrict,pty,command="..."` 模式不现实（我们就是需要全功能 shell），
      但至少做到「只为 unraid-plus 生成一对专用密钥，不共享 root 通用 authorized_keys」。
- [ ] **known_hosts 落盘**：持久化主机密钥指纹（默认即落盘到 `UNRAIDPP_DATA_DIR`），
      重启后能复用确认过的指纹。
- [ ] **日志**：`UNRAIDPP_LOG_LEVEL=info`，不要开 `debug` 进生产（debug 会记录 SSH
      命令的输入输出，可能泄露敏感信息）。
- [ ] **备份 `/boot/config/ssh/authorized_keys`**：切密钥模式前先备份，万一出问题
      可以回滚。

## 7. 已知限制（v0.x）

以下设计在当前版本有弱点，规划在后续迭代加固：

1. **浏览器会话无认证**：v0.x 没有 login，局域网内任何能访问 8080 的人都能用 pool
   里已建立的连接。
   - **缓解**：靠 §4 的链路访问控制，把 8080 限定在内网可信用户。  
   - **路线**：v1.0 加本地管理员密码 + session 超时。

2. **多用户隔离**：当前 pool 是单租户的，只对应一台 Unraid 的一个 SSH 账号。
   多 Unraid 实例 / 多用户共享一台部署的需要自己跑多份。

3. **终端流量明文到后端**：浏览器 ↔ 后端的 WebSocket 在 HTTPS 下是加密的，但
   `/ws/terminal` 自身没有额外加密层。**依赖于反代的 `wss://`**。反代未启用 TLS
   时，终端字节流是明文，**等于把 root shell 暴露在局域网**。

4. **密钥落盘权限**：容器场景下 `/data` 卷的权限取决于挂载时的宿主设置；务必
   确认 `id_ed25519` 文件不能被其他容器或宿主其他用户读取。

5. **没有 CSRF 防护**：API 只接受 application/json POST，浏览器跨站表单不能直接
   打到 JSON 接口；同源 SPA 也没有 cookie 被自动附带攻击面。仍是权宜之计，正式版
   会补 SameSite + CSRF token。

## 8. 漏洞报告

发现安全漏洞请勿直接开 public issue。请私下联系维护者：

- 邮件：N/A（open an issue on GitHub）
- 加密：维护者 PGP 公钥见仓库 `SECURITY.md`（待加入）

我们承诺 72 小时内响应，修复后在 CHANGELOG 致谢（除非报告者要求匿名）。

## 9. 相关文档

- [architecture.md](architecture.md) — 系统架构
- [deployment.md](deployment.md) — 反代 / 容器加固具体写法
- [development.md](development.md) — 开发与调试

> AI生成