# Gotify Notifications — Brainstorming

> Ideas for integrating Gotify push notifications with NodeRouter.

---

## Table of Contents

- [Overview](#overview)
- [Notification Triggers](#notification-triggers)
  - [Critical Alerts](#critical-alerts)
  - [Warning Alerts](#warning-alerts)
  - [Info Alerts](#info-alerts)
- [Implementation Approach](#implementation-approach)
- [Configuration](#configuration)

---

## Overview

Gotify is a self-hosted push notification service that could alert you when important events happen with your nodes. NodeRouter already monitors all services continuously — adding Gotify integration would provide out-of-band alerts without needing to keep the dashboard open.

---

## Notification Triggers

### Critical Alerts

| Trigger | Priority | Message |
|---------|----------|---------|
| **Bitcoin Core disconnects** | 10 (highest) | "Bitcoin Core connection lost: {error}" |
| **Bitcoin Core reconnection** | 8 | "Bitcoin Core reconnected after {duration}" |
| **IBD detected** | 9 | "Bitcoin Core entered Initial Block Download — resyncing from height {height}" |
| **Mempool > 90% full** | 8 | "Mempool at {pct}% capacity — {vsize} vBytes pending" |
| **Monero desync > 10 blocks** | 7 | "Monero node {blocks} blocks behind network" |
| **Fulcrum sync stalled** | 7 | "Fulcrum sync stalled at {height} — Bitcoin at {btc_height}" |

### Warning Alerts

| Trigger | Priority | Message |
|---------|----------|---------|
| **High RPC latency (>500ms)** | 5 | "High latency to {service}: {ms}ms" |
| **Peer count drops below 4** | 5 | "Bitcoin peer count dropped to {count}" |
| **Monero peer count drops to 0** | 5 | "Monero node has no peers" |
| **Difficulty adjustment > ±5%** | 4 | "Difficulty adjustment estimated at {pct}%" |
| **Block not found in >15 min** | 6 | "No new Bitcoin block in 15 minutes" |

### Info Alerts

| Trigger | Priority | Message |
|---------|----------|---------|
| **New block mined** | 1 | "New block #{height} — {txs} txs, {size}MB, {fee} BTC fees" |
| **Config reloaded** | 2 | "Configuration reloaded — {changes}" |
| **NodeRouter started** | 2 | "NodeRouter started — monitoring {count} services" |
| **Monero hard fork detected** | 3 | "Monero hard fork #{version} activated at height {height}" |

---

## Implementation Approach

### Option 1: Server-Side (Go)
Add a Gotify client to the Go backend that sends HTTP POST to the Gotify server when triggers fire.

**Pros:**
- Works even when no browser is connected
- Can track state across restarts
- No client-side dependencies

**Cons:**
- Requires Gotify URL and app token in config
- Additional HTTP calls per poll cycle

### Option 2: Client-Side (JS)
Use the browser's Notification API + Gotify's REST API to send push notifications.

**Pros:**
- No server config changes
- Works with PWA (service worker can send notifications when tab is closed)

**Cons:**
- Only works when browser is open
- Requires user permission for notifications

### Recommended: Option 1 (Server-Side)
More reliable for a monitoring dashboard. The Go backend already has all the data and can send notifications independently of any connected clients.

---

## Configuration

```yaml
# Optional Gotify integration
gotify:
  enabled: false
  url: "https://gotify.example.com"
  token: "your-app-token"
  # Minimum priority to send (1-10)
  min_priority: 5
  # Cooldown between same-type alerts (seconds)
  cooldown: 300
```
