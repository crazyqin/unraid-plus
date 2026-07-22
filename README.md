---
AIGC:
  ContentProducer: '001191110102MAD55U9H0F10002'
  ContentPropagator: '001191110102MAD55U9H0F10002'
  Label: '1'
  ProduceID: '1a3c81f2-cf3c-40b3-9c2a-f11ffcaf81d9'
  PropagateID: '1a3c81f2-cf3c-40b3-9c2a-f11ffcaf81d9'
  ReservedCode1: '6cbed0ea-4e66-444d-8828-c8f281209a3d'
  ReservedCode2: '6cbed0ea-4e66-444d-8828-c8f281209a3d'
---

# unraid+

A web-first, self-hosted Unraid server manager.

Connect via SSH with password or key auth, monitor CPU / memory / storage /
Docker / VMs, manage files through SFTP, and open a browser-based terminal —
all from a single dashboard. Supports multi-server, mobile-friendly UI, and
zero-cloud architecture: your data never leaves your LAN.

## Features

- **Dashboard** — Real-time CPU, memory, network, and disk I/O at a glance
- **Storage** — Array status, disk temperatures, SMART health with auto-fallback
- **Docker** — Container list, start/stop, resource stats, and live logs
- **VMs** — KVM virtual machine status and control
- **Files** — SFTP-based file browser with upload, download, preview, rename, mkdir, delete
- **Terminal** — Full SSH terminal in the browser (WebSocket)
- **Multi-server** — Add and switch between multiple Unraid machines
- **Security** — Optional UI password; SSH key rotation (ED25519); AES-GCM encrypted credential storage
- **Mobile-friendly** — Responsive layout works on phones and tablets
- **Zero cloud** — No data ever leaves your network; all communication is direct SSH

## Architecture

```
Browser ──▶ Go Backend (single binary) ──▶ Unraid (SSH)
                  │
                  ├── REST API (Gin)
                  ├── WebSocket terminal
                  └── Embedded React SPA (go:embed)
```

The backend connects to your Unraid server exclusively via SSH. It reads
structured state files from `/usr/local/emhttp/state/` (the same data source
as the official Unraid WebUI) for fast, reliable monitoring without fragile
shell-command parsing.

## Quick Start

### Docker (recommended)

```bash
docker run -d \
  --name unraid-plus \
  -p 9876:9876 \
  -v unraid-plus-data:/data \
  -e UNRAIDPP_UI_PASSWORD=changeme \
  crazyqin/unraid-plus
```

Open `http://localhost:9876` and follow the onboarding wizard.

### Binary

```bash
# Build from source (requires Go 1.23+ and Node 20+)
git clone https://github.com/crazyqin/unraid-plus.git
cd unraid-plus

# Frontend
cd web && pnpm install && pnpm build

# Sync frontend dist for go:embed
cp -r web/dist server/internal/web/dist

# Backend
cd server && go build -o unraid-plus ./cmd/server

# Run
UNRAIDPP_LISTEN=:9876 UNRAIDPP_DATA_DIR=/var/lib/unraid-plus ./unraid-plus
```

## Configuration

| Environment Variable       | Default   | Description                              |
|----------------------------|-----------|------------------------------------------|
| `UNRAIDPP_LISTEN`          | `:9876`   | Listen address (`host:port`)             |
| `UNRAIDPP_DATA_DIR`        | `./data`  | Directory for persistent state           |
| `UNRAIDPP_UI_PASSWORD`     | *(empty)* | Set to enable UI login protection        |
| `UNRAIDPP_LOG_LEVEL`       | `info`    | Log level: debug, info, warn, error      |
| `UNRAIDPP_SESSION_KEY`     | *(random)*| Session encryption key (auto-generated)   |

## Connecting to Unraid

**Password mode** (zero-config): Enter your Unraid IP and root password.
The password is used once to establish the SSH session and is never stored.

**Key mode** (recommended): Upload or paste your SSH private key during setup.
After connecting, use **Settings → Rotate Key** to generate a dedicated
ED25519 key pair — the original password is no longer needed.

## Tech Stack

| Layer    | Technology                               |
|----------|------------------------------------------|
| Frontend | React 18, TypeScript, Tailwind CSS, Vite |
| Backend  | Go 1.23, Gin, gorilla/websocket          |
| Protocol | SSH (golang.org/x/crypto/ssh), SFTP       |

## License

[MIT](LICENSE)

---

[中文说明](README_CN.md)