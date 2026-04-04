# Bitcoin Core — Service Stats

> Documentation of all JSON-RPC calls NodeRouter makes to Bitcoin Core.

---

## Table of Contents

- [Overview](#overview)
- [RPC Calls](#rpc-calls)
  - [getblockchaininfo](#getblockchaininfo)
  - [getnetworkinfo](#getnetworkinfo)
  - [getmempoolinfo](#getmempoolinfo)
  - [getpeerinfo](#getpeerinfo)
  - [uptime](#uptime)
  - [getblockhash](#getblockhash)
  - [getblock](#getblock)
- [Data Usage](#data-usage)

---

## Overview

| Property | Value |
|----------|-------|
| **Protocol** | JSON-RPC 1.0 over HTTP |
| **Authentication** | HTTP Basic Auth (rpc_user:rpc_password) |
| **Default Port** | 8332 (mainnet) |
| **Timeout** | 10 seconds per call |

**Sample curl command:**
```bash
curl http://192.168.0.211:8332 \
  -u satoshi:satoshi \
  -d '{"jsonrpc":"1.0","id":"nr","method":"getblockchaininfo","params":[]}' \
  -H 'Content-Type: application/json'
```

---

## RPC Calls

### getblockchaininfo

**Purpose:** Core blockchain sync status and chain information.

**Sample Response:**
```json
{
  "result": {
    "blocks": 943609,
    "headers": 943609,
    "verificationprogress": 0.999998,
    "size_on_disk": 832950000000,
    "difficulty": 12345678901234,
    "initialblockdownload": false,
    "pruned": false
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `blocks` | Current synced block height |
| `headers` | Known header count (network height) |
| `verificationprogress` | Sync percentage (0-1) |
| `size_on_disk` | Blockchain size on disk (bytes) |
| `difficulty` | Current network difficulty |
| `initialblockdownload` | Whether node is in IBD mode |
| `pruned` | Whether node is pruned |

---

### getnetworkinfo

**Purpose:** Network status and node version.

**Sample Response:**
```json
{
  "result": {
    "subversion": "/Satoshi:30.2.0/",
    "networkactive": true
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `subversion` | Parsed into version string (e.g., `v30.2.0`) |

---

### getmempoolinfo

**Purpose:** Mempool statistics.

**Sample Response:**
```json
{
  "result": {
    "size": 25000,
    "bytes": 22825062,
    "usage": 125676368,
    "maxmempool": 2000000000
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `size` | Number of transactions in mempool |
| `bytes` | Total size of mempool in bytes |
| `usage` | Memory usage of mempool |
| `maxmempool` | Maximum mempool size (bytes) |

---

### getpeerinfo

**Purpose:** Peer topology and connection details.

**Sample Response:**
```json
{
  "result": [
    {
      "addr": "192.168.1.50:8333",
      "subver": "/Satoshi:30.2.0/",
      "inbound": true,
      "pingtime": 0.0012,
      "bytessent": 1234567,
      "bytesrecv": 9876543
    }
  ]
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `addr` | Peer address (used for Tor/Clearnet/I2P classification) |
| `subver` | Peer client version |
| `inbound` | Connection direction |
| `pingtime` | Peer latency (seconds → milliseconds) |
| `bytessent` | Data sent to peer |
| `bytesrecv` | Data received from peer |

---

### uptime

**Purpose:** Node uptime in seconds.

**Sample Response:**
```json
{
  "result": 86400
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `result` | Uptime in seconds (formatted as `Xd Xh Xm`) |

---

### getblockhash

**Purpose:** Get block hash by height (for recent blocks graphic).

**Parameters:** `[height]`

**Sample Response:**
```json
{
  "result": "000000000000000000032535698c5b0c48283b792cf86c1c6e36ff84464de785"
}
```

**Usage:** Called once per block height to get the hash, then passed to `getblock`.

---

### getblock

**Purpose:** Get full block details for the recent blocks graphic.

**Parameters:** `[hash, 2]` — verbosity 2 includes full transaction data.

**Sample Response:**
```json
{
  "result": {
    "height": 943609,
    "size": 1456789,
    "time": 1712134280,
    "tx": [
      { "vout": [{ "value": 0.5 }] }
    ]
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `height` | Block height (displayed as `#943609`) |
| `size` | Block size in bytes (converted to MB) |
| `time` | Block timestamp (converted to age string) |
| `tx` | Transaction list (count = tx count) |

---

## Data Usage

### Sync Progress
- **Source:** `getblockchaininfo`
- **Display:** `blocks / headers (percentage%)`
- **Bar Color:** Green (≥95%), Orange (80-95%), Red (<80%)

### Peer Topology
- **Source:** `getpeerinfo`
- **Display:** Donut chart (Clearnet/Tor/I2P) + peer table
- **Classification:** Address string analysis (`.onion`, `.i2p`, or clearnet)

### Recent Blocks Graphic
- **Source:** `getblockhash` + `getblock` per block
- **Cache:** Server-side cache — only new blocks trigger RPC calls
- **Display:** Horizontal scrollable chain of block tiles

### Mempool Usage
- **Source:** `getmempoolinfo`
- **Display:** Size, memory usage, percentage of maxmempool
