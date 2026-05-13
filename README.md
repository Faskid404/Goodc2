# Goodc2 — Infrastructure Monitor

A real-time system monitoring platform with a Go server, lightweight agents, and a live web dashboard.

---

## Requirements

### Server
- Go 1.22+
- GCC / musl-dev (for SQLite CGO)
- SQLite3

### Agent (Implant)
- Go 1.22+
- Linux (reads `/proc` for real metrics)

### Dashboard
- Node.js 18+ and npm/pnpm (to build the React frontend)
- Or deploy pre-built `dashboard/dist/`

---

## Environment Variables

### Server
| Variable | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP port (set automatically by Render) |
| `JWT_SECRET` | hardcoded fallback | Secret for signing dashboard JWT tokens |
| `AGENT_TOKEN` | hardcoded fallback | Shared token agents use to authenticate |
| `DASH_PASSWORD` | `omowoli12345` | Dashboard login password |
| `DB_PATH` | `c2.db` | Path to SQLite database file |

### Agent
| Variable | Default | Description |
|---|---|---|
| `C2_URL` | `ws://localhost:8080/ws/agent` | WebSocket URL of the server |
| `AGENT_TOKEN` | hardcoded fallback | Must match server's `AGENT_TOKEN` |

---

## Build & Run Locally

### 1. Server
```bash
cd /path/to/Goodc2
go mod tidy
CGO_ENABLED=1 go build -o c2server ./server
DASH_PASSWORD=omowoli12345 AGENT_TOKEN=mytoken JWT_SECRET=mysecret ./c2server
```

### 2. Agent
```bash
CGO_ENABLED=0 go build -o agent ./implant
C2_URL=ws://your-server:8080/ws/agent AGENT_TOKEN=mytoken ./agent
```

### 3. Dashboard (dev)
```bash
cd dashboard
npm install
npm run dev
```

### 4. Dashboard (production build)
```bash
cd dashboard
npm install
npm run build
# outputs to dashboard/dist/ — served automatically by the Go server
```

---

## Deploy to Render

1. Push this repo to GitHub
2. Go to [render.com](https://render.com) → New → Blueprint
3. Connect your GitHub repo — Render reads `render.yaml` automatically
4. Set `DASH_PASSWORD` to your desired password in the Render dashboard
5. After deploy, note the generated `AGENT_TOKEN` from the Render environment tab
6. Build your agent with that token:
   ```bash
   C2_URL=wss://your-app.onrender.com/ws/agent AGENT_TOKEN=<generated> ./agent
   ```

---

## API Endpoints

All `/api/*` routes require `Authorization: Bearer <jwt>` header.  
Get a JWT by calling `POST /api/auth/login` with `{"password": "..."}`.

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Health check (no auth) |
| `POST` | `/api/auth/login` | Get JWT token |
| `GET` | `/api/agents` | List all agents |
| `GET` | `/api/agents/{id}/metrics` | Metrics history |
| `GET` | `/api/agents/{id}/commands` | Command history |
| `POST` | `/api/agents/{id}/commands` | Send a command |
| `GET` | `/api/agents/{id}/events` | Agent event log |
| `GET` | `/api/events` | All events |

## WebSocket Endpoints

| Path | Auth | Description |
|---|---|---|
| `/ws/agent` | `X-Agent-Token` header | Agent connection |
| `/ws/dashboard` | `?token=<jwt>` query param | Live dashboard feed |

## Available Commands

| Type | Payload | Description |
|---|---|---|
| `get_metrics` | — | Immediate full metrics snapshot |
| `get_disk` | — | Per-mount disk stats |
| `get_network` | — | Per-interface network stats |
| `get_procs` | — | Running process count |
| `get_sysinfo` | — | Kernel, uptime, load average |
| `set_beacon` | seconds (5–3600) | Change reporting interval |
| `ping` | — | Round-trip timestamp check |
