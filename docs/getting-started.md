# Getting Started with NodeRouter

> Complete setup guide for deploying and configuring your NodeRouter dashboard.

---

## Table of Contents

- [Prerequisites](#prerequisites)
- [Installation](#installation)
  - [Docker Compose (Recommended)](#docker-compose-recommended)
  - [Manual Docker](#manual-docker)
  - [Local Development](#local-development)
- [Configuration](#configuration)
  - [Global Settings](#global-settings)
  - [Bitcoin Core](#bitcoin-core)
  - [Mempool Space](#mempool-space)
  - [Fulcrum](#fulcrum)
  - [Monero](#monero)
- [Docker Deployment](#docker-deployment)
  - [Bridge Mode (Default)](#bridge-mode-default)
  - [Host Networking (Remote Nodes)](#host-networking-remote-nodes)
- [Environment Variables](#environment-variables)
- [Hot-Reload Configuration](#hot-reload-configuration)
- [Troubleshooting](#troubleshooting)
  - [Cannot Connect to Bitcoin Core](#cannot-connect-to-bitcoin-core)
  - [Config Changes Not Applying](#config-changes-not-applying)
  - [Mempool Fees Showing as Integers](#mempool-fees-showing-as-integers)
  - [Blockchain Graphic Not Visible](#blockchain-graphic-not-visible)

---

## Prerequisites

- Docker & Docker Compose installed
- Access to at least one of: Bitcoin Core, Mempool Space, Fulcrum, or Monero node
- RPC credentials for each service you want to monitor

---

## Installation

You have two options: use the **prebuilt image** from GitHub Container Registry (fastest), or **build locally** from source.

---

### Option 1: Prebuilt Image (Recommended)

Pull the latest prebuilt image directly from GitHub Container Registry. No need to clone the repository.

#### Using Docker Compose

Create a `docker-compose.yml` file:

```yaml
services:
  noderouter:
    image: ghcr.io/byakugan-1/noderouter:latest
    container_name: noderouter
    ports:
      - "5000:5000"
    volumes:
      - ./config.yaml:/app/config.yaml:ro
    restart: unless-stopped
    # Optional: Resource limits
    # deploy:
    #   resources:
    #     limits:
    #       cpus: "0.5"
    #       memory: 128M
    #     reservations:
    #       cpus: "0.1"
    #       memory: 32M
```

The image is hosted at `ghcr.io/byakugan-1/noderouter`.

```bash
# Download the sample config.yaml
curl -O https://raw.githubusercontent.com/byakugan-1/NodeRouter/refs/heads/main/config.yaml

# Edit with your node credentials
nano config.yaml

# Start
docker compose up -d

# View logs
docker compose logs -f
```

#### Using Docker Run

```bash
# Download the sample config.yaml
curl -O https://raw.githubusercontent.com/byakugan-1/NodeRouter/refs/heads/main/config.yaml

# Edit with your node credentials
nano config.yaml

# Run
docker run -d \
  --name noderouter \
  -p 5000:5000 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  --cpus="0.5" \
  --memory="128m" \
  ghcr.io/byakugan-1/noderouter:latest
```

---

### Option 2: Build Locally

Clone the repository and build the image yourself.

#### Using Docker Compose

```bash
# Clone the repository
git clone https://github.com/byakugan-1/NodeRouter && cd NodeRouter

# Edit configuration
nano config.yaml

# Build and start
docker compose up -d --build

# Or download the sample config if you don't have it:
# curl -O https://raw.githubusercontent.com/byakugan-1/NodeRouter/main/config.yaml

# View logs
docker compose logs -f
```

#### Using Docker Build

```bash
# Clone and build
git clone https://github.com/byakugan-1/NodeRouter && cd NodeRouter
docker build -t noderouter:latest .

# Run (config.yaml is already in the repo)
docker run -d \
  --name noderouter \
  -p 5000:5000 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  noderouter:latest
```

---

### Local Development (Go)

Run directly without Docker for development or debugging.

```bash
# Install Go dependencies
go mod download

# Run locally (default port 5000)
go run main.go

# Custom port and config
go run main.go -port 8080 -config /path/to/config.yaml
```

---

## Configuration

Edit `config.yaml` to match your node setup. Changes apply automatically without restart.

### Global Settings

| Setting | Type | Default | Description |
|---------|------|---------|-------------|
| `global_refresh_interval` | int | 15 | Seconds between server polls (10-30 recommended) |
| `favicon` | string | `"bitcoin"` | Browser tab icon (local name or remote URL) |
| `show_latency` | bool | `true` | Show RPC response time badge next to each module's status dot |

### Bitcoin Core

**Required** — NodeRouter needs Bitcoin Core as the foundation.

| Setting | Type | Description |
|---------|------|-------------|
| `enabled` | bool | Enable/disable monitoring |
| `refresh_interval` | int | Override global refresh interval for this module (seconds, optional) |
| `rpc_address` | string | RPC endpoint (include `http://` or `https://` and port, e.g., `http://192.168.1.50:8332` or `https://your-node.example.com:8332`) |
| `rpc_user` | string | RPC username from `bitcoin.conf` |
| `rpc_password` | string | RPC password from `bitcoin.conf` |
| `recent_blocks_count` | int | Number of recent blocks to display (6-30, default 15) |
| `title_link` | string | Optional URL to open when clicking module title |
| `clearnet_address` | string | Public IP:port for wallet pairing |
| `tor_address` | string | Tor hidden service address for wallet pairing |

> **Remote connections:** Use `https://` for encrypted connections to remote nodes. Self-signed certificates are accepted.

### Mempool Space

**Optional** — Requires a running Mempool Space instance.

| Setting | Type | Description |
|---------|------|-------------|
| `enabled` | bool | Enable/disable monitoring |
| `refresh_interval` | int | Override global refresh interval for this module (seconds, optional) |
| `api_endpoint` | string | Mempool API URL (must end with `/api`, e.g., `http://192.168.1.50:4080/api` or `https://mempool.example.com/api`) |
| `subsat` | bool | Enable sub-sat fee precision (0.1 sat/vB floor) |
| `title_link` | string | Optional URL to open when clicking module title |

> **Remote connections:** Both `http://` and `https://` are supported.

### Fulcrum

**Optional** — Requires a running Fulcrum Electrum Server.

| Setting | Type | Description |
|---------|------|-------------|
| `enabled` | bool | Enable/disable monitoring |
| `refresh_interval` | int | Override global refresh interval for this module (seconds, optional) |
| `rpc_address` | string | Fulcrum server IP/hostname |
| `rpc_port` | int | Fulcrum TCP/SSL port (50001 TCP, 50002 SSL) |
| `ssl_enabled` | bool | Use SSL connection |
| `title_link` | string | Optional URL to open when clicking module title |
| `clearnet_address` | string | Public IP:port for Electrum wallet pairing |
| `tor_address` | string | Tor address for Electrum wallet pairing |

> **Remote connections:** Set `ssl_enabled: true` with port 50002 for encrypted connections to remote Fulcrum servers.

### Monero

**Optional** — Requires a running monerod with RPC enabled.

| Setting | Type | Description |
|---------|------|-------------|
| `enabled` | bool | Enable/disable monitoring |
| `refresh_interval` | int | Override global refresh interval for this module (seconds, optional) |
| `rpc_address` | string | Monero RPC endpoint (include `http://` or `https://` and port, e.g., `http://192.168.1.50:18081` or `https://monero.example.com:18081`) |
| `recent_blocks_count` | int | Number of recent blocks to display (6-30, default 15) |
| `title_link` | string | Optional URL to open when clicking module title |

> **Remote connections:** Use `https://` for encrypted connections to remote nodes. Self-signed certificates are accepted.

---

## Docker Deployment

### Bridge Mode (Default)

Use when your nodes are accessible from the Docker network.

```yaml
# docker-compose.yml
ports:
  - "5000:5000"
```

Access dashboard at `http://localhost:5000`

### Host Networking (Remote Nodes)

Use when connecting to nodes on other machines/networks.

```yaml
# docker-compose.yml
# ports:
#   - "5000:5000"
network_mode: host
```

Access dashboard at `http://<HOST_IP>:5000`

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NODEROUTER_CONFIG` | Path to config file | `/app/config.yaml` |
| `NODEROUTER_PORT` | Server port | `5000` |

---

## Hot-Reload Configuration

NodeRouter watches `config.yaml` for changes. When you edit the file:

1. Changes are detected automatically via `fsnotify`
2. Config is reloaded without restart
3. All connected clients receive a `config_changed` event and refresh
4. Log entry shows exactly which settings changed

**No container restart needed** — just edit the file and save.

---

## Troubleshooting

### Cannot Connect to Bitcoin Core

1. Verify `rpc_address` includes `http://` prefix
2. Check `rpcallowip` in `bitcoin.conf` includes the container IP
3. Ensure `rpc_user` and `rpc_password` match `bitcoin.conf`
4. Test connectivity: `curl http://<rpc_address> -u <user>:<pass> -d '{"jsonrpc":"1.0","id":"1","method":"getblockchaininfo","params":[]}'`

### Config Changes Not Applying

1. NodeRouter uses file watching — changes should apply automatically
2. Check container logs: `docker logs noderouter`
3. Verify the config file is mounted correctly: `docker exec noderouter cat /app/config.yaml`

### Mempool Fees Showing as Integers

1. Set `subsat: true` in the mempool section of `config.yaml`
2. Ensure your Mempool Space instance supports the `/api/v1/fees/precise` endpoint

### Blockchain Graphic Not Visible

1. Ensure Bitcoin Core is running and synced
2. Check that `recent_blocks_count` is between 6-30
3. Verify the container has network access to Bitcoin Core RPC
