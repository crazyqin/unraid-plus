---
AIGC:
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: 'c2b55054-ecea-4647-9f1a-1a24edc686fc'
  PropagateID: 'c2b55054-ecea-4647-9f1a-1a24edc686fc'
  ReservedCode1: '29e36ca8-38d3-448b-a11f-95c5ccd2457f'
  ReservedCode2: '29e36ca8-38d3-448b-a11f-95c5ccd2457f'
---

# unraid++

> 一款更易上手的 Unraid 服务器管理器 · 网页端优先 · 后续支持 Android / iOS / 鸿蒙

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Frontend](https://img.shields.io/badge/Frontend-React_18-61dafb.svg)](web)
[![Backend](https://img.shields.io/badge/Backend-Go-00add8.svg)](server)
[![Deploy](https://img.shields.io/badge/Deploy-Docker-2496ed.svg)](docker-compose.yml)

`unraid++` 受 [Easy Unraid](https://github.com/wlaosj/easy-unraid-releases) 启发，目标是把 Unraid 服务器的日常管理做得**对新手更友好、对极客更透明**。

## 与 Easy Unraid 的差异

| 维度 | Easy Unraid | unraid++ |
| --- | --- | --- |
| 形态 | Flutter 原生客户端（5 端） | 网页端优先，后续原生扩展 |
| 上手门槛 | 需自行配置 SSH 密钥对 | 默认密码模式，**零配置开箱即用** |
| 部署 | 各平台分别下载安装包 | **一键 `docker compose up`** |
| 价格 | 免费 + Pro 付费 | **完全免费开源**（Apache 2.0） |
| 引导 | 无 | **内嵌新手引导与术语解释** |
| 通信 | 客户端直连 Unraid IP | 后端中转（可独立部署或装为 Unraid 插件） |

## 核心功能路线

- **📊 实时仪表盘**：CPU 负载 / 各核温度 / 内存 / 网速 / 阵列读写，图形化看板
- **🐳 Docker 管理**：容器启停 / 实时日志 / Compose 编排 / YAML 编辑
- **💾 存储监控**：磁盘空间 / 温度 / SMART 健康告警
- **📁 文件管理**：基于 SFTP，上传 / 下载 / 在线预览 / 解压
- **🖥️ SSH 终端**：浏览器内 WebSocket 终端，多会话
- **🚀 虚拟机控制**：VM 启停 / 状态查看
- **🧭 新手引导**：首次进入零门槛向导，关键术语悬浮解释

## 快速开始

### 一键 Docker 部署（推荐）

```bash
git clone https://github.com/your-org/unraid-plus-plus.git
cd unraid-plus-plus
docker compose up -d
```

打开浏览器访问 `http://<部署机 IP>:8080`，按引导填入 Unraid 地址与 root 密码即可。

### 本地开发

详见 [docs/development.md](docs/development.md)。

- 前端：`cd web && pnpm install && pnpm dev`
- 后端：`cd server && go run ./cmd/server`

## 技术栈

- **前端**：React 18 · TypeScript · Vite · Tailwind CSS · shadcn/ui · Zustand · TanStack Query · xterm.js
- **后端**：Go · Gin · `golang.org/x/crypto/ssh` · SFTP · WebSocket · 单二进制
- **部署**：多阶段 Docker 构建，最终镜像 ~25MB，ARM64 / AMD64 双架构

## 架构

```
浏览器 ──HTTP/WS──▶ unraid++ 后端（Go）──SSH/SFTP──▶ Unraid 服务器
                         │
                         └──HTTP──▶ Unraid 原生 API（/webgui/api）
```

后端是浏览器与 Unraid 之间的「安全代理 + 协议翻译层」：浏览器永远不直连 SSH，所有 SSH/SFTP 会话都由后端发起并通过 WebSocket 转发。

详细架构见 [docs/architecture.md](docs/architecture.md)。

## 路线图

- [x] v0.1 项目骨架（前端 + 后端 + Docker）
- [ ] v0.2 仪表盘 + 系统信息
- [ ] v0.3 Docker 容器管理
- [ ] v0.4 文件管理 + SFTP
- [ ] v0.5 SSH 终端
- [ ] v0.6 存储监控 + VM 控制
- [ ] v0.7 新手引导完善
- [ ] v1.0 网页端 GA
- [ ] 后续 Android / iOS / 鸿蒙原生客户端

## 安全说明

- root 密码默认仅存在于后端内存，可启用「密钥对模式」彻底免密
- 所有通信建议走 HTTPS / WireGuard / Tailscale，**不要把管理端口直接暴露公网**
- 详见 [docs/security.md](docs/security.md)

## License

[Apache License 2.0](LICENSE) © unraid++ contributors

本项目与 Easy Unraid（wlaosj）项目无关联、无派生关系，仅在产品形态上受其启发。

> AI生成