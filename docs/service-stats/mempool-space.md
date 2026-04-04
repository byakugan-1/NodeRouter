# Mempool Space — Service Stats

> Documentation of all REST API calls NodeRouter makes to Mempool Space.

---

## Table of Contents

- [Overview](#overview)
- [API Calls](#api-calls)
  - [/api/v1/fees/recommended](#apiv1feesrecommended)
  - [/api/v1/fees/precise](#apiv1feesprecise)
  - [/api/v1/fees/mempool-blocks](#apiv1feesmempool-blocks)
  - [/api/mempool](#apimempool)
  - [/api/v1/difficulty-adjustment](#apiv1difficulty-adjustment)
  - [/api/v1/prices](#apiv1prices)
- [Data Usage](#data-usage)

---

## Overview

| Property | Value |
|----------|-------|
| **Protocol** | REST API over HTTPS |
| **Authentication** | None (public API) |
| **Default Port** | 4080 (self-hosted) |
| **Timeout** | 10 seconds per call |
| **Rate Limits** | May be enforced on public instances |

**Sample curl command:**
```bash
curl http://192.168.0.211:4080/api/v1/fees/recommended
```

---

## API Calls

### /api/v1/fees/recommended

**Purpose:** Recommended fee estimates (integer sat/vB).

**Sample Response:**
```json
{
  "fastestFee": 2,
  "halfHourFee": 1,
  "hourFee": 1,
  "economyFee": 1,
  "minimumFee": 1
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `fastestFee` | Highest priority fee (sat/vB) |
| `halfHourFee` | ~30 min confirmation fee |
| `hourFee` | ~60 min confirmation fee |
| `economyFee` | Low priority fee |
| `minimumFee` | Minimum relay fee |

---

### /api/v1/fees/precise

**Purpose:** Recommended fee estimates with sub-sat precision (0.1 sat/vB floor).

**Enabled when:** `subsat: true` in config.

**Sample Response:**
```json
{
  "fastestFee": 2.023,
  "halfHourFee": 1.094,
  "hourFee": 0.502,
  "economyFee": 0.2,
  "minimumFee": 0.1
}
```

**Fields Used:** Same as `/fees/recommended`, but with decimal precision.

---

### /api/v1/fees/mempool-blocks

**Purpose:** Projected next mempool block statistics.

**Sample Response:**
```json
[
  {
    "blockSize": 873046,
    "blockVSize": 746096.5,
    "nTx": 863,
    "totalFees": 8875608,
    "medianFee": 10.79,
    "feeRange": [1, 2.4, 8.1, 10.1, 11.0, 12.0, 14.9, 302.1]
  }
]
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `medianFee` | Next block median fee (displayed as "Next Fee") |
| `nTx` | Transaction count in next block |

---

### /api/mempool

**Purpose:** Current mempool backlog statistics.

**Sample Response:**
```json
{
  "count": 3169,
  "vsize": 1891542,
  "total_fee": 20317481,
  "fee_histogram": []
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `count` | Number of unconfirmed transactions |
| `vsize` | Total virtual size of mempool (bytes) |
| `total_fee` | Total fees in mempool (satoshis) |

---

### /api/v1/difficulty-adjustment

**Purpose:** Current difficulty epoch progress and estimated change.

**Sample Response:**
```json
{
  "progressPercent": 44.39,
  "difficultyChange": 98.45,
  "estimatedRetargetDate": 1627762478,
  "remainingBlocks": 1121,
  "remainingTime": 665977,
  "previousRetarget": -4.80,
  "nextRetargetHeight": 741888,
  "timeAvg": 302328,
  "adjustedTimeAvg": 302328,
  "timeOffset": 0
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `progressPercent` | Epoch progress percentage |
| `difficultyChange` | Estimated difficulty change percentage |
| `remainingBlocks` | Blocks remaining until next adjustment |

---

### /api/v1/prices

**Purpose:** Latest BTC price in major currencies.

**Sample Response:**
```json
{
  "time": 1703252411,
  "USD": 43753,
  "EUR": 40545,
  "GBP": 37528,
  "CAD": 58123,
  "CHF": 37438,
  "AUD": 64499,
  "JPY": 6218915
}
```

**Fields Used:**
| Field | Usage |
|-------|-------|
| `USD` | BTC price in USD (displayed in price row) |

---

## Data Usage

### Fee Estimates
- **Source:** `/v1/fees/recommended` or `/v1/fees/precise`
- **Display:** 4-tier fee card (No Priority, Hour, Half Hour, Fastest)
- **Precision:** Integer or decimal based on `subsat` config

### Mempool Block Projection
- **Source:** `/v1/fees/mempool-blocks`
- **Display:** Next block median fee + transaction count

### Epoch Progress
- **Source:** `/v1/difficulty-adjustment`
- **Display:** Progress %, estimated change %, blocks remaining

### BTC Price
- **Source:** `/v1/prices`
- **Display:** USD price in header row
