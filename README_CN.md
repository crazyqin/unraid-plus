---
AIGC:
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '374001c7-2d8f-475d-8787-29ea9a1942b9'
  PropagateID: '374001c7-2d8f-475d-8787-29ea9a1942b9'
  ReservedCode1: 'a1432593-49c2-4309-a117-4f5cfaaed074'
  ReservedCode2: 'a1432593-49c2-4309-a117-4f5cfaaed074'
---

# unraid+

一个基于 Web 的 Unraid 服务器管理器，通过 SSH 直连你的 NAS，无需安装任何客户端。

通过密码或密钥认证连接 SSH，监控 CPU / 内存 / 存储 / Docker / 虚拟机，
基于 SFTP 管理文件，在浏览器中打开 SSH 终端 —— 全部集中在一个仪表盘中。
支持多服务器、移动端友好、零云架构：你的数据绝不离开局域网。

## 主要功能

- **仪表盘** — 实时监控 CPU、内存、网络、磁盘读写
- **存储管理** — 阵列状态、磁盘温度、SMART 健康检测（自动降级）
- **Docker 管理** — 容器列表、启停、资源占用、实时日志流
- **虚拟机管理** — KVM 虚拟机状态和控制
- **文件管理** — 基于 SFTP 的文件浏览器，支持上传/下载/预览/重命名/新建/删除
- **Web 终端** — 浏览器内 SSH 命令行（WebSocket）
- **多服务器** — 添加和切换多台 Unraid 机器
- **安全** — 可选界面密码、SSH 密钥轮换（ED25519）、AES-GCM 加密存储凭据
- **移动端友好** — 响应式布局，手机平板均可使用
- **零云架构** — 所有数据不离开局域网，直连 SSH

## 架构

```
浏览器 ──▶ Go 后端（单二进制文件） ──▶ Unraid (SSH)
                  │
                  ├── REST API (Gin)
                  ├── WebSocket 终端
                  └── 嵌入式 React SPA (go:embed)
```

后端通过 SSH 直连 Unraid 服务器，读取 `/usr/local/emhttp/state/` 下的结构化状态文件
（与官方 Unraid WebUI 相同的数据源），快速、稳定，无需解析脆弱的 shell 命令输出。

## 快速开始

### Docker（推荐）

```bash
docker run -d \
  --name unraid-plus \
  -p 9876:9876 \
  -v unraid-plus-data:/data \
  -e UNRAIDPP_UI_PASSWORD=changeme \
  crazyqin/unraid-plus
```

打开 `http://localhost:9876`，按向导输入 Unraid 的 IP 和密码即可接入。

### 从源码构建

```bash
# 需要 Go 1.23+ 和 Node 20+
git clone https://github.com/crazyqin/unraid-plus.git
cd unraid-plus

# 前端
cd web && pnpm install && pnpm build

# 同步前端构建产物用于 go:embed
cp -r web/dist server/internal/web/dist

# 后端
cd server && go build -o unraid-plus ./cmd/server

# 运行
UNRAIDPP_LISTEN=:9876 UNRAIDPP_DATA_DIR=/var/lib/unraid-plus ./unraid-plus
```

## 配置

| 环境变量                   | 默认值     | 说明                          |
|----------------------------|-----------|-------------------------------|
| `UNRAIDPP_LISTEN`          | `:9876`   | 监听地址 (`host:port`)        |
| `UNRAIDPP_DATA_DIR`        | `./data`  | 持久化数据目录                |
| `UNRAIDPP_UI_PASSWORD`     | *(空)*    | 设置后启用界面登录保护        |
| `UNRAIDPP_LOG_LEVEL`       | `info`    | 日志级别：debug, info, warn, error |
| `UNRAIDPP_SESSION_KEY`     | *(随机)*  | 会话加密密钥（自动生成）      |

## 连接 Unraid

**密码模式**（零配置）：输入 Unraid IP 和 root 密码，密码仅用于建立 SSH 会话，不会存储。

**密钥模式**（推荐）：在设置时上传或粘贴 SSH 私钥。连接后可使用 **设置 → 轮换密钥**
生成专用 ED25519 密钥对，之后不再需要密码。

## 技术栈

| 层     | 技术                                     |
|--------|------------------------------------------|
| 前端   | React 18, TypeScript, Tailwind CSS, Vite |
| 后端   | Go 1.23, Gin, gorilla/websocket          |
| 协议   | SSH (golang.org/x/crypto/ssh), SFTP       |

## 许可证

[MIT](LICENSE)

---

[English](README.md)