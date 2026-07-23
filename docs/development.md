
---
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '187f25b7-341a-420b-a6ce-19360c296afd'
  PropagateID: '187f25b7-341a-420b-a6ce-19360c296afd'
  ReservedCode1: 'daa27431-88d9-4d46-8d58-c7341c2ecc95'
  ReservedCode2: 'daa27431-88d9-4d46-8d58-c7341c2ecc95'
---

---
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '2be182a5-b546-4583-9036-d650074be3d6'
  PropagateID: '2be182a5-b546-4583-9036-d650074be3d6'
  ReservedCode1: '1a149900-ecb0-4127-8e49-c408b443e509'
  ReservedCode2: '1a149900-ecb0-4127-8e49-c408b443e509'
---

# 开发指南（Development）

本文档说明如何在本地把 unraid-plus 跑起来、修改代码、调试，以及提交贡献时需要遵守的
约定。

## 1. 前置依赖

| 工具 | 版本 | 用途 |
| --- | --- | --- |
| Node.js | ≥ 20.18（推荐 20 LTS） | 前端构建 |
| pnpm | ≥ 9（核心开发者用 9.12.0，Dockerfile 内固定） | 前端包管理 |
| Go | ≥ 1.23（CI 用 1.23-alpine） | 后端编译 |
| Git | 任意 | 版本控制 |
| Docker（可选） | ≥ 24 | 一键构建最终镜像 |

Windows 用户可用 nvm-windows / fnm 装 Node，Go 直接下 MSI 即可。pnpm 通过
`corepack enable && corepack prepare pnpm@9.12.0 --activate` 启用。

> 国内开发者建议设置 Go 代理：`go env -w GOPROXY=https://goproxy.cn,direct`，
> `GOSUMDB=off`，避免拉不到 golang.org/x/crypto。

## 2. 项目布局

```
unraid-plus/
├─ web/                # React 前端
│  ├─ src/
│  ├─ dist/            # 构建产物（后端 go:embed 会用到）
│  ├─ package.json
│  ├─ tsconfig*.json
│  ├─ vite.config.ts
│  └─ tailwind.config.js
├─ server/             # Go 后端
│  ├─ cmd/server/main.go
│  ├─ internal/
│  │  ├─ api/          # 路由、handler、中间件
│  │  ├─ ssh/          # Pool / Client / SFTP / TerminalHub
│  │  ├─ unraid/       # Unraid 原生 API 封装
│  │  ├─ web/          # go:embed dist/ 与 SPA 中间件
│  │  ├─ config/       # 环境变量解析
│  │  └─ auth/ ws/     # 预留
│  ├─ pkg/logger/
│  ├─ go.mod / go.sum
│  └─ .temp/           # 本地构建产物（gitignored）
├─ docs/               # 本目录
├─ Dockerfile          # 三阶段构建
├─ docker-compose.yml
├─ README.md
└─ LICENSE
```

`.temp/` 目录是 agent / 本地工具用的临时区，已被 `.gitignore`，**不要把构建产物
提交上去**。

## 3. 本地开发工作流

### 3.1 同时跑前后端（推荐）

前端 Vite dev server 自带 `/api` 与 `/ws` 代理到 `:8080`，所以最舒服的循环是两个
终端各开一个：

```bash
# 终端 A：后端（编译快，直接 go run）
cd server
go run ./cmd/server
# → listening on :8080

# 终端 B：前端（热更新）
cd web
pnpm install        # 首次
pnpm dev
# → Local: http://localhost:5173/
```

浏览器开 `http://localhost:5173/`。所有页面请求会被 Vite 代理到后端 8080。引导向导
里填入你 Unraid 服务器的 IP / root 密码即可开始使用。

> 如果暂时没有真实 Unraid，可以填任何 SSH 可达的 Linux 主机，部分页面（文件、终端、
> 仪表盘的 CPU/内存/网络部分）依然能跑通，Docker / 阵列 / VM 相关接口会返回空。

### 3.2 只跑前端

为UI 验收而前端后端没有，可直接：

```bash
cd web && pnpm dev
```

Vite 代理会失败，但页面布局和交互仍可调。

### 3.3 只跑后端（验证 SPA 走通）

把前端 dist 拷贝到 `server/internal/web/dist/` 后端就会内嵌并提供：

```bash
cd web && pnpm build
# 把 dist 拷到 server/internal/web/dist/（脚本或手动 rsync / Robocopy）
cd ../server
go run ./cmd/server
# 打开 http://localhost:8080
```

### 3.4 构建生产二进制

```bash
cd web && pnpm build
# 同步 dist 到 server/internal/web/dist/

cd ../server
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.Version=dev -X main.Commit=$(git rev-parse --short HEAD 2>/dev/null || echo none)" \
  -o .temp/unraid-plus ./cmd/server
```

产物在 `server/.temp/unraid-plus`，体积约 14 MB（含前端）。

## 4. 代码约定

### 4.1 前端

- **语言**：TypeScript strict；`pnpm typecheck` 必须通过。
- **样式**：Tailwind 工具类优先；设计 token（颜色、圆角、阴影）放在
  `src/styles/globals.css` 的 CSS 变量里，支持亮 / 暗色。
- **状态**：本地组件用 useState；跨页面共享用 Zustand store；服务端缓存用
  TanStack Query。**不要**把服务端数据塞进 Zustand。
- **i18n**：UI 文案为简体中文；代码标识符与注释用英文。
- **组件**：原子组件放 `components/ui/`，按 shadcn/ui 规范；业务组件就近放
  `components/<feature>/` 或页面目录。

### 4.2 后端

- **Go 版本**：1.23 起步，以 Dockerfile 中 `golang:1.23-alpine` 为准。
- **module path**：`github.com/crazyqin/unraid-plus/server`。
- **分层**：`cmd/` 只做装配；业务在 `internal/`；可被外部引用的放 `pkg/`。
  **不要**让 `internal/api` 直接调用 `golang.org/x/crypto/ssh`——一律走
  `internal/ssh.Client`。
- **错误处理**：handler 返回 `c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})`；
  底层错误用 `fmt.Errorf("xxx: %w", err)` 包裹。
- **日志**：统一用 `pkg/logger`，不要 `log.Println` 或 `fmt.Println`。
- **注释**：导出标识符必须有 godoc 注释，以标识符本身开头。

### 4.3 通用

- 提交前自检：
  - `cd web && pnpm typecheck && pnpm build`
  - `cd ../server && go vet ./... && go build ./cmd/server`
- 提交信息用约定式提交（`feat:` / `fix:` / `docs:` / `refactor:` / `chore:`）。
- PR 不要带 `.temp/` 内容，也不要带重新生成的 `web/dist/` 提交（CI 会自己 build）。

## 5. 调试技巧

### 5.1 后端日志

```bash
UNRAIDPP_LOG_LEVEL=debug go run ./cmd/server
```

`debug` 级会打印每条 SSH 命令的发送与返回摘要，调试命令解析非常有用。

### 5.2 手动测 SSH 命令

在 Unraid 上手敲同样命令确认输出格式，再到 dashboard.go 等文件里写解析。例如：

```bash
ssh root@<unraid> 'cat /proc/stat | head -n 1'
ssh root@<unraid> 'docker ps --format "{{json .}}"'
```

### 5.3 前端 devtools

- React DevTools：看组件树与 props。
- TanStack Query Devtools：`queryClient` 已内置，开 dev 模式可装插件。
- 网络：看 `/api/*` 的请求 / 响应；WS 走 Network → WS 帧查看。

### 5.4 终端 WS 帧

xterm 不走 REST，调试时把页面切到 Terminal，在 DevTools Network 里选中 `terminal`
连接，Frames 面板可以看到双向文本帧（注意其中可能有 ANSI 转义，看着乱是正常的）。

## 6. 测试（规划中）

当前 v0.x 阶段还没有正式测试套件。计划：

- 后端：把 SSH 命令解析函数（`parseMeminfo` / `computeDiskRW` / `parseNetDev` …）
  抽成 pure function，用样例字符串做表驱动测试。SSH 协议层用
  [gliderlabs/ssh](https://github.com/gliderlabs/ssh) 起本地 SSH server 做集成测试。
- 前端：关键 store（onboarding 流转、settings 持久化）用 vitest；UI 组件用
  @testing-library/react。E2E 暂不引入，避免 CI 太重。

在此之前，**修改命令解析逻辑时，至少保留一份真实输出贴在注释或 PR 描述里**，
方便后人对照。

## 7. 常见坑

- **`pnpm build` 报 `Cannot find module 'X'`**：通常 lock 文件和 package.json 不一致，
  跑 `pnpm install` 再试。
- **`go vet` 报 `package X is not in std`**：Go 安装不完整（src/ 缺文件），重装 Go。
- **前端代理 404**：确认后端真的在 :8080；`vite.config.ts` 中 proxy target 配的就是
  `http://localhost:8080`。
- **`go:embed` 报 `pattern dist: no matching files`**：`server/internal/web/dist/`
  里没有内容，需要先 `pnpm build` 并同步过来；目录至少要有一个 `.gitkeep`。
- **WS 终端连不上**：浏览器控制台看 WS 是不是直接连到 `:5173`（dev）或 `:8080`
  （prod），路由器 / 反代可能没透传 `Upgrade` 头。

## 8. 相关文档

- [architecture.md](architecture.md) — 系统架构与请求路径
- [deployment.md](deployment.md) — 部署到生产
- [security.md](security.md) — 安全模型

> AI生成