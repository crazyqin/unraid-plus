
# 部署指南（Deployment）

本文档说明把 unraid-plus 跑到生产环境的几种方式、环境变量清单，以及更新与反代配置。

## 1. 部署方式总览

| 方式 | 适合场景 | 复杂度 | 推荐 |
| --- | --- | --- | --- |
| Docker Compose | 单机、Unraid 用户、家庭 NAS | ★ | ⭐ 首选 |
| 单 Docker 容器 | 已有 compose / k8s 编排 | ★★ | |
| 裸二进制 + systemd | 不方便装 Docker 的机器 | ★★★ | |
| 安装到 Unraid 本机（插件） | 想让管理器和 NAS 同机 | ★★ | 后期规划 |

所有方式最终都是跑同一个二进制 `unraid-plus`，默认监听 `:8080`，**不要直接暴露公网**
（见 [security.md](security.md)）。

## 2. Docker Compose（推荐）

仓库根目录已提供 `docker-compose.yml`：

```bash
git clone https://github.com/crazyqin/unraid-plus.git
cd unraid-plus
docker compose up -d
```

默认从源码构建（三阶段 Dockerfile，最终镜像 ~25 MB）。也可改用预构建镜像：把
`build:` 段注释、`image:` 行取消注释：

```yaml
services:
  unraid-plus:
    image: ghcr.io/crazyqin/unraid-plus:latest
    container_name: unraid-plus
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      UNRAIDPP_LISTEN: ":8080"
      UNRAIDPP_DATA_DIR: "/data"
      TZ: Asia/Shanghai
    volumes:
      - unraid-plus-data:/data

volumes:
  unraid-plus-data:
```

打开 `http://<部署机 IP>:8080`，引导向导里填 Unraid IP + root 密码即可。

### 2.1 多架构镜像

Dockerfile 是多阶段的，CI 里构建时用 `docker buildx` 同时出 amd64 / arm64：

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/crazyqin/unraid-plus:latest \
  --push .
```

Unraid 多数跑在 Intel（amd64），少数装在树莓派 / 瑞芯微 SoC（arm64）上，双架构能覆盖。

### 2.2 使用 host 网络模式

如果你的 NAS 与 unraid-plus 同机，或想省一层端口转发，可用 `network_mode: host`：

```yaml
services:
  unraid-plus:
    # …
    network_mode: host
    # ports: 段需要去掉
```

此时容器监听的就是宿主机 8080。

## 3. 单 Docker 容器（已有编排）

```bash
docker volume create unraid-plus-data
docker run -d \
  --name unraid-plus \
  --restart unless-stopped \
  -p 8080:8080 \
  -e UNRAIDPP_LISTEN=:8080 \
  -e UNRAIDPP_DATA_DIR=/data \
  -e TZ=Asia/Shanghai \
  -v unraid-plus-data:/data \
  ghcr.io/crazyqin/unraid-plus:latest
```

## 4. 裸二进制 + systemd

适合不方便跑 Docker 的场景。

### 4.1 编译

在任意装了 Go 1.23+ 与 Node 20+ 的机器上：

```bash
# 前端
cd web && pnpm install --frozen-lockfile && pnpm build

# 把 dist 同步到 server/internal/web/dist/
rsync -a dist/ ../server/internal/web/dist/

# 后端（按目标平台设 GOOS / GOARCH）
cd ../server
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags "-s -w -X main.Version=v0.1" \
  -o /tmp/unraid-plus ./cmd/server
```

### 4.2 systemd 单元

把二进制放到 `/usr/local/bin/unraid-plus`，配置目录 `/var/lib/unraid-plus`：

```ini
# /etc/systemd/system/unraid-plus.service
[Unit]
Description=unraid-plus server
After=network-online.target

[Service]
Type=simple
User=unraid-plus
Group=unraid-plus
ExecStart=/usr/local/bin/unraid-plus
Environment=UNRAIDPP_LISTEN=:8080
Environment=UNRAIDPP_DATA_DIR=/var/lib/unraid-plus
Environment=UNRAIDPP_LOG_LEVEL=info
Environment=TZ=Asia/Shanghai
Restart=on-failure
RestartSec=3

# 加固
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/unraid-plus

[Install]
WantedBy=multi-user.target
```

```bash
sudo useradd -r -d /var/lib/unraid-plus -s /usr/sbin/nologin unraid-plus
sudo mkdir -p /var/lib/unraid-plus && sudo chown unraid-plus:unraid-plus /var/lib/unraid-plus
sudo systemctl daemon-reload
sudo systemctl enable --now unraid-plus
sudo journalctl -u unraid-plus -f
```

## 5. 环境变量参考

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `UNRAIDPP_LISTEN` | `:8080` | 监听地址 |
| `UNRAIDPP_DATA_DIR` | `./data` | 配置 / 缓存目录（known_hosts、可选密钥） |
| `UNRAIDPP_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |
| `UNRAIDPP_DEFAULT_HOST` | 空 | 预填主机，跳过引导 |
| `UNRAIDPP_DEFAULT_PORT` | `22` | 预填端口 |
| `UNRAIDPP_DEFAULT_USER` | `root` | 预填用户名 |
| `UNRAIDPP_DEFAULT_API` | 空 | Unraid webgui API URL（可选） |
| `UNRAIDPP_DEFAULT_PASSWD` | 空 | 预填密码（**不推荐**用环境变量传敏感信息，仅供无人值守初始化） |
| `UNRAIDPP_SESSION_KEY` | 启动时随机 | Session 签名密钥。固定后重启会话不丢失 |
| `TZ` | `Asia/Shanghai`（容器） | 时区，影响日志时间戳 |

> `UNRAIDPP_DEFAULT_PASSWD` 只在「首次部署后无人值守自动连」时使用，密码一旦连上
> 会进入 pool 内存；建议连完后改用密钥模式并清掉这个变量。详见
> [security.md](security.md)。

## 6. 反向代理

强烈建议在 unraid-plus 前面套一层 HTTPS 反代（Caddy / Nginx / Traefik）。原因：
- 浏览器到后端的流量包含 root 密码与终端字节流，**必须加密**。
- 现代浏览器对非 HTTPS 的 WebSocket 越来越严格，`wss://` 才稳。

### 6.1 Caddy（最省心）

```caddyfile
unraid-plus.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

Caddy 自动签 Let's Encrypt 证书，WebSocket 反代开箱即用。

### 6.2 Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name unraid-plus.example.com;

    ssl_certificate     /etc/ssl/private/fullchain.pem;
    ssl_certificate_key /etc/ssl/private/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # WebSocket（终端 / 未来的日志流）
    location /ws/ {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 3600s;
    }
}
```

`proxy_read_timeout` 拉长是为了让空闲终端不被断。

### 6.3 一键 TLS

如果不想买域名，最简单的内网方案是在 Unraid 上装 Tailscale，在 Tailscale 网内用
`http://<tailnet-name>:8080` 访问——Tailscale 自带端到端加密，省去证书烦恼。

## 7. 升级

### Docker / Compose

```bash
docker compose pull
docker compose up -d
```

由于配置保存在 `unraid-plus-data` 卷里，升级不丢引导状态；前端 onboarding 标记在
浏览器 localStorage 里，也不受影响。

### 裸二进制

```bash
sudo systemctl stop unraid-plus
sudo mv /usr/local/bin/unraid-plus /usr/local/bin/unraid-plus.bak
sudo install -m 0755 unraid-plus /usr/local/bin/unraid-plus
sudo systemctl start unraid-plus
sudo journalctl -u unraid-plus -f --since "1 min ago"
```

## 8. 数据与卸载

`UNRAIDPP_DATA_DIR` 里只有：
- `known_hosts`（可选）— 主机密钥缓存
- `id_ed25519` / `id_ed25519.pub`（可选）— 用户切换到密钥模式后生成的密钥对
- 服务自身的 session 状态

**不会保存任何 Unraid root 密码**（除非显式启用密钥落盘）。

完全卸载：

```bash
# Docker
docker compose down -v

# systemd
sudo systemctl disable --now unraid-plus
sudo rm /usr/local/bin/unraid-plus /etc/systemd/system/unraid-plus.service
sudo rm -rf /var/lib/unraid-plus
sudo systemctl daemon-reload
sudo userdel unraid-plus
```

## 9. 相关文档

- [architecture.md](architecture.md) — 系统架构
- [development.md](development.md) — 本地开发
- [security.md](security.md) — 安全模型与加固