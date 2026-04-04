# Fulcrum — Service Stats

> Documentation of all JSON-RPC 2.0 calls NodeRouter makes to Fulcrum Electrum Server.

---

## Table of Contents

- [Overview](#overview)
- [RPC Calls](#rpc-calls)
  - [server.version](#serverversion)
  - [blockchain.headers.subscribe](#blockchainheaderssubscribe)
- [Data Usage](#data-usage)

---

## Overview

| Property | Value |
|----------|-------|
| **Protocol** | JSON-RPC 2.0 over TCP or SSL |
| **Authentication** | None |
| **Default Ports** | 50001 (TCP), 50002 (SSL) |
| **Timeout** | 10 seconds per call |
| **Connection** | Persistent TCP/SSL connection per poll cycle |

**Sample curl command (via netcat):**
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"server.version","params":["NodeRouter","1.4"]}' | nc 192.168.0.211 50001
```

---

## RPC Calls

### server.version

**Purpose:** Server identification and version string.

**Parameters:** `["NodeRouter", "1.4"]` — client name and protocol version.

**Sample Response:**
```json
{
  "result": "Fulcrum 2.1.0",
  "id": 1
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `result` | Server version string (displayed in Fulcrum module) |

---

### blockchain.headers.subscribe

**Purpose:** Current header height (sync progress).

**Parameters:** `[]` — no parameters needed.

**Sample Response:**
```json
{
  "result": {
    "height": 943609
  },
  "id": 1
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `result.height` | Fulcrum's current header height |

---

## Data Usage

### Sync Progress
- **Source:** `blockchain.headers.subscribe` (height) compared against Bitcoin Core's `headers` count
- **Calculation:** `(fulcrum.height / bitcoin.headers) * 100`
- **Display:** `height / bitcoin_headers (percentage%)`
- **Bar Color:** Green (≥95%), Orange (80-95%), Red (<80%)

### Version Display
- **Source:** `server.version`
- **Display:** Version string in Fulcrum module
