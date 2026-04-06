# Notifications — Service Stats

> Documentation of the Gotify notification system and all API endpoints NodeRouter uses for notifications.

---

## Table of Contents

- [Overview](#overview)
- [How It Works](#how-it-works)
- [Gotify API Integration](#gotify-api-integration)
- [Mempool Space API for Notifications](#mempool-space-api-for-notifications)
- [Notification Types](#notification-types)
- [Privacy & Security](#privacy--security)
- [API Endpoints](#api-endpoints)
- [Settings UI](#settings-ui)

---

## Overview

| Property | Value |
|----------|-------|
| **Notification Provider** | Gotify (self-hosted push notification server) |
| **Protocol** | HTTPS POST to Gotify `/message` endpoint |
| **Authentication** | Gotify app token via `X-Gotify-Key` header |
| **Check Frequency** | Configurable via `notifications.check_freq` in config.yaml (10-300 seconds, default 30s) |
| **TXID Tracking** | Uses Mempool Space `/api/tx/{txid}` REST endpoint |

NodeRouter's notification system mimics the design of [Mempal](https://github.com/aeonBTC/Mempal), an Android app that monitors the Bitcoin mempool. The same polling patterns and notification logic have been adapted for NodeRouter's web dashboard.

---

## How It Works

1. **Configuration**: Users configure Gotify credentials and notification preferences via the Settings modal (accessed from the footer).
2. **Persistence**: Settings are saved to `config.yaml` and hot-reloaded without restart.
3. **Polling**: A dedicated goroutine checks notification conditions at the configured `check_freq` interval (separate from dashboard refresh).
4. **Notification**: When a condition is met, NodeRouter sends a POST request to the Gotify server.
5. **State Tracking**: Notification state (e.g., "already notified") is kept in memory only — never logged or persisted to disk.
6. **Privacy**: TXIDs are never logged. After a TX is confirmed and the user is notified, the TX watch entry is marked as notified and can be cleared at any time.
7. **Auto-Clear**: Specific block height notifications are automatically cleared after they fire (the setting resets to allow reconfiguration).

---

## Gotify API Integration

### Send Message

**Endpoint:** `POST {gotify_url}/message`

**Headers:**
```
X-Gotify-Key: {your_app_token}
Content-Type: application/json
```

**Request Body:**
```json
{
  "title": "Fee Rate Alert",
  "message": "Fee rate has fallen below 5.0 sat/vB and is currently at 3.2 sat/vB",
  "priority": 5
}
```

**Response:** `200 OK` on success.

**Priority Levels Used:**
| Priority | Use Case |
|----------|----------|
| 3 | New block mined (informational) |
| 5 | Fee rate alerts (moderate) |
| 8 | TX confirmed / specific block reached (important) |

---

## Mempool Space API for Notifications

### Fee Rates

**Endpoint:** `GET {api_endpoint}/api/v1/fees/recommended` (or `/v1/fees/precise` if subsat enabled)

**Used For:** Fee rate threshold monitoring. NodeRouter checks the fastest fee rate against the user's threshold.

> **Note:** The `/api` prefix is automatically appended if not present in the configured endpoint.

### Mempool Size

**Endpoint:** `GET {api_endpoint}/api/mempool`

**Used For:** Mempool size threshold monitoring. The `vsize` field is converted to vMB (virtual megabytes) and compared against the threshold.

### Block Height

**Endpoint:** Derived from Bitcoin Core's `getblockchaininfo` RPC (blocks field)

**Used For:** New block detection and specific block height monitoring.

### Transaction Status

**Endpoint:** `GET {api_endpoint}/api/tx/{txid}`

**Request:**
```bash
curl http://192.168.0.211:4080/api/tx/{txid}
```

**Sample Response (confirmed):**
```json
{
  "txid": "abc123...",
  "status": {
    "confirmed": true,
    "block_height": 943609,
    "block_hash": "000000000000000000032535698c5b0c48283b792cf86c1c6e36ff84464de785",
    "block_time": 1712134280
  }
}
```

**Sample Response (unconfirmed):**
```json
{
  "txid": "abc123...",
  "status": {
    "confirmed": false
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `status.confirmed` | Whether the TX has been included in a block |
| `status.block_height` | The block height where the TX was confirmed |

---

## Notification Types

### 1. Fee Rate Alerts
- **What:** Notifies when the fastest fee rate rises above or falls below a threshold.
- **Mimics:** Mempal's `checkFeeRates()` in [`NotificationService.kt`](../mempal/app/src/main/java/com/example/mempal/service/NotificationService.kt)
- **Logic:** Checks every check_freq cycle. Only notifies once per threshold crossing (resets when fee moves back to normal).

### 2. New Block Notifications
- **What:** Notifies every time a new block is mined.
- **Mimics:** Mempal's `checkNewBlocks()` in [`NotificationService.kt`](../mempal/app/src/main/java/com/example/mempal/service/NotificationService.kt)
- **Logic:** Compares current block height to the last known height. Fires on every new block.

### 3. Specific Block Height
- **What:** Notifies when a specific block height is reached.
- **Mimics:** Mempal's specific block height check in [`checkNewBlocks()`](../mempal/app/src/main/java/com/example/mempal/service/NotificationService.kt)
- **Logic:** One-shot notification. Once the block is reached, the setting is automatically cleared (disabled and height reset to 0).

### 4. TX Confirmation Tracking
- **What:** Notifies when a specific TXID reaches a target number of confirmations.
- **Mimics:** Mempal's `checkTransactionConfirmation()` in [`NotificationService.kt`](../mempal/app/src/main/java/com/example/mempal/service/NotificationService.kt)
- **Logic:** Polls `/api/tx/{txid}` every check_freq cycle. When confirmed and confirmations >= target, sends notification and marks as notified.

---

## Privacy & Security

### TXID Privacy
- **TXIDs are NEVER logged** — not in server logs, not in browser console, not in any persistent storage.
- TXIDs are stored only in server memory during runtime.
- After notification, TX watch entries are marked as notified and can be cleared via the UI.
- The "Clear All TX Watches" button removes all TX entries from memory instantly.
- When displaying TXIDs in notifications, only the first 4 and last 4 characters are shown (e.g., `abc1...de78`).

### Gotify URL Security
- Gotify URLs are validated to prevent SSRF (Server-Side Request Forgery).
- Internal/private IP addresses are blocked (10.x, 172.16-31.x, 192.168.x, 127.x, 169.254.x).
- Cloud metadata endpoints are blocked (169.254.169.254, metadata.google.internal).

### Token Security
- Gotify tokens are stored in `config.yaml` (same as other credentials).
- Tokens are visible in the settings UI as plain text for easy verification.
- Tokens are returned in the API response for live-sync with the UI.

---

## API Endpoints

### GET /api/notifications

Returns current notification and service connection settings.

**Response:**
```json
{
  "refresh_interval": 10,
  "show_latency": true,
  "btc_blocks_count": 30,
  "xmr_blocks_count": 15,
  "conn_btc_rpc": "http://192.168.0.211:8332",
  "conn_btc_user": "satoshi",
  "conn_btc_pass": "satoshi",
  "svc_mp_enabled": true,
  "conn_mp_api": "http://192.168.0.211:4080",
  "svc_ful_enabled": true,
  "conn_fulcrum": "192.168.0.211:50001",
  "svc_xmr_enabled": true,
  "conn_xmr_rpc": "http://192.168.0.211:18089",
  "gotify_url": "https://gotify.example.com",
  "gotify_token": "your_app_token",
  "gotify_configured": true,
  "notif_enabled": true,
  "check_freq": 30,
  "fee_notif_enabled": true,
  "fee_threshold": 5.0,
  "fee_above_threshold": false,
  "new_block_notif": true,
  "specific_block_notif": false,
  "specific_block_height": 0,
  "tx_watches": [],
  "tx_target_confs": 1
}
```

### POST /api/notifications

Saves settings, tests Gotify, tests service connections, or manages TX watches.

**Actions:**

| Action | Description | Required Fields |
|--------|-------------|-----------------|
| `save` | Save all notification and service settings | All setting fields |
| `test` | Send a test notification to Gotify | `gotify_url`, `gotify_token` |
| `test_connection` | Test a service connection | `test_name`, `test_url` |
| `clear_tx` | Remove all TX watch entries from memory | None |
| `remove_tx` | Remove a specific TXID from watches | `txid` |

**Save Request Body:**
```json
{
  "action": "save",
  "refresh_interval": 10,
  "show_latency": true,
  "btc_blocks_count": 30,
  "xmr_blocks_count": 15,
  "conn_btc_rpc": "http://192.168.0.211:8332",
  "conn_btc_user": "satoshi",
  "conn_btc_pass": "satoshi",
  "svc_mp_enabled": true,
  "conn_mp_api": "http://192.168.0.211:4080",
  "svc_ful_enabled": true,
  "conn_fulcrum": "192.168.0.211:50001",
  "svc_xmr_enabled": true,
  "conn_xmr_rpc": "http://192.168.0.211:18089",
  "gotify_url": "https://gotify.example.com",
  "gotify_token": "your_token_here",
  "notif_enabled": true,
  "check_freq": 30,
  "fee_notif_enabled": true,
  "fee_threshold": 5.0,
  "fee_above_threshold": false,
  "new_block_notif": true,
  "specific_block_notif": false,
  "specific_block_height": 0,
  "tx_target_confs": 1
}
```

**Test Connection Request Body:**
```json
{
  "action": "test_connection",
  "test_name": "mempool",
  "test_url": "http://192.168.0.211:4080"
}
```

> **Note:** For Mempool connections, `/api` is automatically appended if not present.

**Response:**
```json
{
  "success": true,
  "message": "Settings saved"
}
```

---

## Settings UI

The Settings modal is accessed from the footer and contains two collapsible sections:

### Global Settings
- **Refresh Interval**: How often the dashboard fetches new data (5-120 seconds)
- **Show Latency Badges**: Toggle response time display
- **Bitcoin Recent Blocks Count**: Number of blocks to display (6-30)
- **Monero Recent Blocks Count**: Number of blocks to display (6-30)

### Service Connections
- **Bitcoin Core**: RPC Address, User, Password (always enabled)
- **Mempool Space**: API Endpoint (toggle enable/disable)
- **Fulcrum**: Address:Port (toggle enable/disable)
- **Monero Node**: RPC Address (toggle enable/disable)

Each service has a "Test Connection" button to verify connectivity.

### Notifications
- **Enable Notifications**: Master toggle for Gotify notifications
- **Gotify Server**: URL and Token fields
- **Check Frequency**: How often to check notification conditions (10-300 seconds)
- **Fee Rate Alerts**: Threshold monitoring for fastest fee rate
- **Block Notifications**: New block and specific block height alerts
- **TX Confirmation Tracking**: Watch transactions for confirmations

---

## Configuration (config.yaml)

All settings are stored in `config.yaml`:

### Global Settings
```yaml
global_refresh_interval: 10
show_latency: true
```

### Service Connections
```yaml
bitcoin_core:
  enabled: true
  rpc_address: "http://192.168.0.211:8332"
  rpc_user: "satoshi"
  rpc_password: "satoshi"
  recent_blocks_count: 30

mempool:
  enabled: true
  api_endpoint: "http://192.168.0.211:4080"
  subsat: true

fulcrum:
  enabled: true
  rpc_address: "192.168.0.211"
  rpc_port: 50001
  ssl_enabled: false

monero:
  enabled: true
  rpc_address: "http://192.168.0.211:18089"
  recent_blocks_count: 15
```

### Notifications
```yaml
notifications:
  gotify_url: "https://gotify.example.com"
  gotify_token: "your_app_token"
  enabled: true
  check_freq: 30
```
