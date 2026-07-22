
---
AIGC:
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '03dc0358-6dc5-4426-a240-b53e6a864d66'
  PropagateID: '03dc0358-6dc5-4426-a240-b53e6a864d66'
  ReservedCode1: '7f3bff43-be0e-42ac-851a-13263d2ad88a'
  ReservedCode2: '7f3bff43-be0e-42ac-851a-13263d2ad88a'
---

# 架构设计（Architecture）

本文档描述 unraid-plus 的整体架构、组件协作方式、关键请求路径与数据流。面向想要理解
内部实现、贡献代码或做二次集成开发者。

## 1. 高层架构

```
┌─────────────┐       HTTP / WebSocket        ┌──────────────────┐    SSH / SFTP    ┌──────────────┐
│   浏览器     │ ───────────────────────────▶ │ unraid-plus 后端  │ ───────────────▶ │   Unraid     │
│ (React SPA) │ ◀─────────────────────────── │   (Go, 单二进制)  │ ◀─────────────── │   服务器     │
└─────────────┘      REST + xterm WS          └──────────────────┘   HTTP /webgui   └──────────────┘
                          ▲                           │
                          │                           │ go:embed dist/
                          │                           ▼
                          │                  ┌──────────────────┐
                          └──────────────────│  内嵌前端静态资源 │
                                             └──────────────────┘
```

- **浏览器**：纯前端 SPA，只和后端通信，**永远不直连 Unraid 的 SSH 端口**。
- **后端**：唯一持有 SSH/SFTP 凭据的进程，是浏览器的「安全代理 + 协议翻译层」。
  编译时用 `go:embed all:dist` 把前端构建产物打进单个二进制，运行时一并托管。
- **Unraid**：标准 SSH（22）+ 原生 webgui API。后端对它发起出站连接。

## 2. 组件分层

### 2.1 前端（`web/`）

| 目录 / 文件 | 职责 |
| --- | --- |
| `src/main.tsx` `App.tsx` | 入口、路由（`/onboarding` + `AppLayout` 嵌套路由） |
| `src/pages/*` | 八个主页面：Onboarding / Dashboard / Docker / Storage / Files / Terminal / Vms / Settings |
| `src/components/layout/*` | 应用骨架：Sidebar、TopBar、AppLayout |
| `src/components/ui/*` | shadcn/ui 风格原子组件（button、card、dialog、…） |
| `src/stores/{auth,settings,onboarding}.ts` | Zustand 状态，settings/onboarding 持久化到 localStorage |
| `src/lib/{api,queryClient,utils}.ts` | API 客户端、TanStack Query 实例、工具函数 |
| `src/types/index.ts` | 与后端 DTO 对齐的 TypeScript 类型 |

前端只通过两种信道与后端打交道：
1. **REST**：`/api/*`，TanStack Query 负责缓存与轮询（如仪表盘 2s 节拍）。
2. **WebSocket**：`/ws/terminal`，浏览器侧由 xterm.js 渲染；用于多会话 SSH 终端。

### 2.2 后端（`server/`）

| 包 | 职责 |
| --- | --- |
| `cmd/server/main.go` | 进程入口：加载配置、构建 Pool/Hub、自动重连、优雅关闭 |
| `internal/config` | 环境变量驱动的运行时配置 |
| `internal/api` | `router.go` 装配路由 + 中间件；`handler/*` 是各业务 handler；`middleware/` 请求日志 |
| `internal/ssh` | `pool.go` 连接池、`client.go` ssh.Client 封装、`hostkey.go` TOFU 主机密钥、`sftp.go` SFTP 包装、`terminal.go` WS↔PTY 多路复用 |
| `internal/unraid` | Unraid 原生 webgui HTTP API 的薄封装 |
| `internal/web` | `embed.go` 用 `go:embed` 暴露 dist；`dist/` 是构建时拷入的前端产物 |
| `pkg/logger` | 统一结构化日志 |

### 2.3 关键运行时对象

- **`ssh.Pool`**：以 `host:port` 为键缓存若干 `ssh.Client`。每个 host 一条长连接，
  命令复用、SFTP 子系统按需开启。`Pool.Connect` 时执行 TOFU 主机密钥校验并把
  Unraid 版本号探测出来。
- **`ssh.TerminalHub`**：多路复用 WebSocket ↔ SSH PTY。浏览器一个 WS 连接 = 一个
  SSH 交互会话（`RequestPty` + `Shell`）。hub 负责把前端发的字节写进 `sess.StdinPipe()`，
  把 `StdoutPipe` / `StderrPipe` 的字节回写 WS。
- **`unraid.Client`**：可选路径。当 Unraid 启用了 webgui API（需应用密钥）时，后端
  会走它拉取一些 SSH 不好拿的状态（比如部分 VM 信息）。**未配置密钥时此降级到纯 SSH。**

## 3. 典型请求路径

### 3.1 连接建立（Onboarding / 首次配置）

```
浏览器 POST /api/connect {host, port, user, password|privateKey}
  └─▶ handler.Connect
        └─▶ pool.Connect(cfg)
              ├─ ssh.Dial(host:port, sshCfg)         # 含 TCP + SSH 握手
              ├─ knownHosts.callback(...)            # TOFU 校验
              └─ client.Run("cat /etc/unraid-version")  # 健康探测
  ◀─── {hostFingerprint, serverVersion}
```

成功后连接进入 pool；失败返回结构化错误供前端引导用户重试。密码默认仅保存在
**内存**里（参见 [security.md](security.md)）。

### 3.2 仪表盘轮询

```
浏览器 GET /api/dashboard （每 2s）
  └─▶ handler.Dashboard
        ├─ cli.Run("cat /proc/stat")          # ×2，间隔 ~900ms 算 delta
        ├─ cli.Run("cat /proc/meminfo")
        ├─ cli.Run("cat /proc/net/dev")       # ×2
        ├─ cli.Run("cat /proc/diskstats")     # ×2
        ├─ cli.Run("cat /proc/uptime" "/proc/loadavg")
        └─ cli.Run("for z in /sys/class/thermal/thermal_zone*; do cat $z/temp; done")
  ◀─── JSON: {cpu, memory, network, arrayRwBytesPerSec, uptime, loadAvg}
```

为减少 SSH 会话抖动，所有命令复用同一条 pool 连接里的独立 session。
采样间隔写死 900ms——这是个权衡值，再小会拖慢响应、再大会错过突发尖峰。

### 3.3 终端会话

```
浏览器 new WebSocket("/ws/terminal")
  └─▶ api.serveTerminal → upgrader.Upgrade
        └─▶ hub.Serve(conn)
              ├─ 读取首条消息里的 {cols, rows}
              ├─ cli.NewInteractiveSession(cols, rows)  # RequestPty + Shell
              └─ goroutine A: WS→Stdin
                 goroutine B: Stdout/Stderr→WS
                 goroutine C: 对端关闭/WS 关闭 → 资源回收
```

resize 消息走带类型的 WS 帧，到达后转 `sess.WindowChange`。

### 3.4 文件管理（SFTP）

```
浏览器 GET /api/files?path=/mnt/user/...
  └─▶ handler.ListFiles
        └─▶ cli.SFTP() → (*SFTPClient)
              └─ sftp.ReadDir / Stat
  ◀─── {entries: [{name, size, mode, modTime, isDir}, ...]}

浏览器 POST /api/files/delete {paths: [...]}
  └─▶ 路径白名单校验：必须位于 /mnt、/root、/tmp 之一
  └─▶ sftp.Remove / 递归删除
```

### 3.5 Docker / Storage / VMs

这三个页面共享一种「`cli.Run` 跑 `docker ...` / `smartctl ...` / `virsh ...`，再用
`unmarshalLooseJSON` 或字符串切分解析」的模式。日志走 `cli.RunStream(cmd, w)` 把容器
`docker logs -f` 的输出通过 `SSE` / WS 流式回传。

## 4. 数据流与状态边界

| 数据 | 在前端 | 在后端 | 持久化 |
| --- | --- | --- | --- |
| Unraid 凭据（密码 / 私钥） | 引导时输入一次，之后不存储 | 内存 (`managedConn.password` / `privateKey`) | **默认不落盘**；密钥模式可显式写 `/data` |
| 主机密钥指纹 | 展示用 | in-memory `knownHosts` cache | 可选落盘 `/data/known_hosts` |
| 用户设置（主题、技能等级、轮询频率） | Zustand + localStorage | — | 浏览器 localStorage |
| Onboarding 完成标记 | localStorage | — | 浏览器 localStorage |
| 会话 Cookie | gin session | `SessionKey`（环境变量或启动时随机） | 重启若无 `UNRAIDPP_SESSION_KEY` 则失效 |

## 5. 部署形态

unraid-plus 后端可以三种方式部署，前端都是同一个二进制内嵌的资源：

1. **独立容器**（推荐）：`docker compose up`，暴露 8080。
2. **Unraid 插件**：作为 Community Applications 里的一个插件安装到 NAS 本机，
   此时后端和 Unraid 同主机，SSH 走 `127.0.0.1`。
3. **裸二进制**：把编译出来的 `unraid-plus` 放到任意 Linux/ARM 机器，配合 systemd。

详见 [deployment.md](deployment.md)。

## 6. 扩展点

未来要加新功能时，通常走以下三处之一：

- **新加一个 shell 命令 → 新加一个 handler**：仿照 `dashboard.go` 里的 `cli.Run` +
  `parseXxx` 模式，在 `handler/` 下新增文件，在 `router.go` 注册路由。
- **新加一个 WebSocket 信道**（如 Docker 日志流）：在 `internal/ssh` 加 hub，在
  `router.go` 加 `/ws/xxx` 路由，前端在对应页面里 `new WebSocket`。
- **接入 Unraid 原生 API**：扩展 `internal/unraid/client.go`，由 handler 在 SSH
  数据不全时按需回落。

## 7. 相关文档

- [development.md](development.md) — 本地开发与调试
- [deployment.md](deployment.md) — 部署与环境变量
- [security.md](security.md) — 安全模型与加固建议

> AI生成