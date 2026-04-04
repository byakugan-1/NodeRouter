# Monero — Service Stats

> Documentation of all REST API and JSON-RPC 2.0 calls NodeRouter makes to Monero (monerod).

---

## Table of Contents

- [Overview](#overview)
- [API Calls](#api-calls)
  - [/get_info (REST)](#get_info-rest)
  - [/json_rpc get_version](#json_rpc-get_version)
  - [/json_rpc get_block_headers_range](#json_rpc-get_block_headers_range)
- [Data Usage](#data-usage)

---

## Overview

| Property | Value |
|----------|-------|
| **Protocol** | REST API + JSON-RPC 2.0 over HTTP |
| **Authentication** | None (or basic auth if configured) |
| **Default Port** | 18081 (mainnet), 18089 (restricted) |
| **Timeout** | 10-15 seconds per call |

**Sample curl command:**
```bash
curl http://192.168.0.211:18089/get_info
```

---

## API Calls

### /get_info (REST)

**Purpose:** Core node status, sync progress, and network statistics.

**Sample Response:**
```json
{
  "height": 3644960,
  "target_height": 3644960,
  "difficulty": 641097890630,
  "tx_count": 59428095,
  "tx_pool_size": 26,
  "database_size": 273804165120,
  "incoming_connections_count": 0,
  "outgoing_connections_count": 0,
  "nettype": "mainnet",
  "synchronized": true,
  "start_time": 1611915662,
  "status": "OK",
  "version": ""
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `height` | Current synced block height |
| `target_height` | Network height (for sync %) |
| `difficulty` | Network difficulty (displayed as T/G/M) |
| `tx_count` | Total transactions since genesis |
| `tx_pool_size` | Number of unconfirmed transactions |
| `database_size` | LMDB database size (bytes) |
| `incoming_connections_count` | Inbound peer count |
| `outgoing_connections_count` | Outbound peer count |
| `nettype` | Network type (mainnet/testnet/stagenet) |
| `synchronized` | Whether node is fully synced |
| `start_time` | Daemon start time (Unix timestamp) |

---

### /json_rpc get_version

**Purpose:** Software version number.

**Sample Request:**
```bash
curl -X POST http://192.168.0.211:18089/json_rpc \
  -d '{"jsonrpc":"2.0","id":"0","method":"get_version"}' \
  -H 'Content-Type: application/json'
```

**Sample Response:**
```json
{
  "result": {
    "version": 196623,
    "release": true,
    "status": "OK"
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `version` | Decoded as major.minor.patch (e.g., `0.18.3.15`) |

**Decoding Logic:**
```go
major := (verNum >> 16) & 0xFF
minor := (verNum >> 8) & 0xFF
patch := verNum & 0xFF
```

---

### /json_rpc get_block_headers_range

**Purpose:** Fetch recent block headers for the blockchain graphic.

**Sample Request:**
```bash
curl -X POST http://192.168.0.211:18089/json_rpc \
  -d '{"jsonrpc":"2.0","id":"0","method":"get_block_headers_range","params":{"start_height":3644945,"end_height":3644959}}' \
  -H 'Content-Type: application/json'
```

**Sample Response:**
```json
{
  "result": {
    "headers": [
      {
        "height": 3644945,
        "timestamp": 1775291678,
        "block_weight": 139036,
        "num_txes": 64
      }
    ],
    "status": "OK"
  }
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `height` | Block height (displayed as `#3644945`) |
| `timestamp` | Block timestamp (converted to age string) |
| `block_weight` | Block weight in bytes (converted to MB) |
| `num_txes` | Transaction count in block |

**Note:** This is a highly efficient single RPC call that returns N block headers at once. NodeRouter uses this to fetch the entire recent blocks range in one call.

---

## Data Usage

### Sync Progress
- **Source:** `/get_info` (`height` vs `target_height`)
- **Calculation:** `(height / target_height) * 100`
- **Display:** `height / target_height (percentage%)`
- **Bar Color:** Green (≥95%), Orange (80-95%), Red (<80%)

### Recent Blocks Graphic
- **Source:** `/json_rpc get_block_headers_range`
- **Cache:** Full range refresh each poll cycle (efficient single RPC call)
- **Display:** Horizontal scrollable chain of block tiles

### Network Stats
- **Source:** `/get_info`
- **Display:** Difficulty, Total TXs, Database Size, TX Pool
