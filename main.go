package main

import (
	"bufio"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	qrcode "github.com/skip2/go-qrcode"
	"gopkg.in/yaml.v3"
)

//go:embed templates/*.html static/*
var embeddedFiles embed.FS

// ==================== Config ====================

type Config struct {
	GlobalRefreshInterval int        `yaml:"global_refresh_interval"`
	Favicon               string     `yaml:"favicon"`
	ShowLatency           bool       `yaml:"show_latency"`
	BitcoinCore           BitcoinCfg `yaml:"bitcoin_core"`
	Mempool               MempoolCfg `yaml:"mempool"`
	Fulcrum               FulcrumCfg `yaml:"fulcrum"`
	Monero                MoneroCfg  `yaml:"monero"`
}

type BitcoinCfg struct {
	Enabled           bool   `yaml:"enabled"`
	RPCAddress        string `yaml:"rpc_address"`
	RPCUser           string `yaml:"rpc_user"`
	RPCPassword       string `yaml:"rpc_password"`
	ClearnetAddress   string `yaml:"clearnet_address"`
	TorAddress        string `yaml:"tor_address"`
	TitleLink         string `yaml:"title_link"`
	RecentBlocksCount int    `yaml:"recent_blocks_count"`
}

type MempoolCfg struct {
	Enabled     bool   `yaml:"enabled"`
	APIEndpoint string `yaml:"api_endpoint"`
	TitleLink   string `yaml:"title_link"`
	Subsat      bool   `yaml:"subsat"`
}

type FulcrumCfg struct {
	Enabled         bool   `yaml:"enabled"`
	RPCAddress      string `yaml:"rpc_address"`
	RPCPort         int    `yaml:"rpc_port"`
	SSLEnabled      bool   `yaml:"ssl_enabled"`
	ClearnetAddress string `yaml:"clearnet_address"`
	TorAddress      string `yaml:"tor_address"`
	TitleLink       string `yaml:"title_link"`
}

type MoneroCfg struct {
	Enabled           bool   `yaml:"enabled"`
	RPCAddress        string `yaml:"rpc_address"`
	TitleLink         string `yaml:"title_link"`
	RecentBlocksCount int    `yaml:"recent_blocks_count"`
}

var (
	cfgMu         sync.RWMutex
	cfg           *Config
	cfgPath       string
	faviconVer    int64 // increments on each config reload for cache-busting
)

// faviconMap maps known icon names to their local filenames in static/logos/
var faviconMap = map[string]string{
	"bitcoin":     "bitcoin.svg",
	"fulcrum":     "fulcrum.png",
	"mempool":     "mempool.svg",
	"monero":      "monero.png",
	"bitcoin.png": "bitcoin.svg",
}

// dashboardIconsCDN is the base URL for the homarr-labs dashboard-icons CDN
const dashboardIconsCDN = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons/png/"

func loadConfig() error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	c := &Config{GlobalRefreshInterval: 15, ShowLatency: true}
	if err := yaml.Unmarshal(data, c); err != nil {
		return err
	}
	if c.GlobalRefreshInterval <= 0 {
		c.GlobalRefreshInterval = 15
	}
	if !c.BitcoinCore.Enabled {
		log.Printf("[Config] Warning: Bitcoin Core is disabled. NodeRouter requires Bitcoin Core for full functionality.")
	}
	// Validate Bitcoin Core recent_blocks_count (6-30, default 15)
	if c.BitcoinCore.RecentBlocksCount < 6 {
		c.BitcoinCore.RecentBlocksCount = 15
	}
	if c.BitcoinCore.RecentBlocksCount > 30 {
		c.BitcoinCore.RecentBlocksCount = 30
	}
	// Validate Monero recent_blocks_count (6-30, default 15)
	if c.Monero.RecentBlocksCount < 6 {
		c.Monero.RecentBlocksCount = 15
	}
	if c.Monero.RecentBlocksCount > 30 {
		c.Monero.RecentBlocksCount = 30
	}
	cfgMu.Lock()
	cfg = c
	faviconVer = time.Now().Unix() // increment version for cache-busting
	cfgMu.Unlock()
	log.Printf("[Config] Loaded from %s (interval: %ds)", cfgPath, c.GlobalRefreshInterval)
	return nil
}

func getConfig() *Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return cfg
}

func getFaviconVersion() int64 {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return faviconVer
}

// resolveFavicon returns the URL path for the favicon.
// Priority: remote URL (http/https) > dashboard-icons CDN name > local embedded icon > default bitcoin logo
func resolveFavicon(favicon string) string {
	if favicon == "" {
		return "/static/logos/bitcoin.svg"
	}
	// Remote URL (including dashboard-icons CDN URLs)
	if strings.HasPrefix(favicon, "http://") || strings.HasPrefix(favicon, "https://") {
		return favicon
	}
	// Check if it's a known local icon
	if fname, ok := faviconMap[favicon]; ok {
		return "/static/logos/" + fname
	}
	// If it has an extension, treat it as a dashboard-icons CDN name
	if strings.HasSuffix(favicon, ".png") || strings.HasSuffix(favicon, ".svg") || strings.HasSuffix(favicon, ".ico") {
		// Extract the name without extension for CDN lookup
		name := strings.TrimSuffix(favicon, ".png")
		name = strings.TrimSuffix(name, ".svg")
		name = strings.TrimSuffix(name, ".ico")
		return dashboardIconsCDN + name + ".png"
	}
	// Treat as a dashboard-icons name (without extension)
	return dashboardIconsCDN + favicon + ".png"
}

func watchConfig() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer watcher.Close()
	if err := watcher.Add(cfgPath); err != nil {
		return
	}
	log.Printf("[Config] Watching %s", cfgPath)
	var prevCfg *Config
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				time.Sleep(200 * time.Millisecond)
				if err := loadConfig(); err != nil {
					log.Printf("[Config] Reload error: %v", err)
				} else {
					// Log what changed
					cur := getConfig()
					if prevCfg != nil {
						var changes []string
						if cur.GlobalRefreshInterval != prevCfg.GlobalRefreshInterval {
							changes = append(changes, fmt.Sprintf("refresh_interval: %ds → %ds", prevCfg.GlobalRefreshInterval, cur.GlobalRefreshInterval))
						}
						if cur.Favicon != prevCfg.Favicon {
							changes = append(changes, fmt.Sprintf("favicon: %q → %q", prevCfg.Favicon, cur.Favicon))
						}
						if cur.BitcoinCore.RecentBlocksCount != prevCfg.BitcoinCore.RecentBlocksCount {
							changes = append(changes, fmt.Sprintf("recent_blocks_count: %d → %d", prevCfg.BitcoinCore.RecentBlocksCount, cur.BitcoinCore.RecentBlocksCount))
						}
						if cur.BitcoinCore.RPCAddress != prevCfg.BitcoinCore.RPCAddress {
							changes = append(changes, "bitcoin_core.rpc_address changed")
						}
						if cur.BitcoinCore.RPCUser != prevCfg.BitcoinCore.RPCUser {
							changes = append(changes, "bitcoin_core.rpc_user changed")
						}
						if cur.BitcoinCore.RPCPassword != prevCfg.BitcoinCore.RPCPassword {
							changes = append(changes, "bitcoin_core.rpc_password changed")
						}
						if cur.BitcoinCore.ClearnetAddress != prevCfg.BitcoinCore.ClearnetAddress {
							changes = append(changes, "bitcoin_core.clearnet_address changed")
						}
						if cur.BitcoinCore.TorAddress != prevCfg.BitcoinCore.TorAddress {
							changes = append(changes, "bitcoin_core.tor_address changed")
						}
						if cur.BitcoinCore.TitleLink != prevCfg.BitcoinCore.TitleLink {
							changes = append(changes, "bitcoin_core.title_link changed")
						}
						if cur.Mempool.Enabled != prevCfg.Mempool.Enabled {
							changes = append(changes, fmt.Sprintf("mempool.enabled: %v → %v", prevCfg.Mempool.Enabled, cur.Mempool.Enabled))
						}
						if cur.Mempool.APIEndpoint != prevCfg.Mempool.APIEndpoint {
							changes = append(changes, "mempool.api_endpoint changed")
						}
						if cur.Mempool.Subsat != prevCfg.Mempool.Subsat {
							changes = append(changes, fmt.Sprintf("mempool.subsat: %v → %v", prevCfg.Mempool.Subsat, cur.Mempool.Subsat))
						}
						if cur.Mempool.TitleLink != prevCfg.Mempool.TitleLink {
							changes = append(changes, "mempool.title_link changed")
						}
						if cur.Fulcrum.Enabled != prevCfg.Fulcrum.Enabled {
							changes = append(changes, fmt.Sprintf("fulcrum.enabled: %v → %v", prevCfg.Fulcrum.Enabled, cur.Fulcrum.Enabled))
						}
						if cur.Fulcrum.RPCAddress != prevCfg.Fulcrum.RPCAddress {
							changes = append(changes, "fulcrum.rpc_address changed")
						}
						if cur.Fulcrum.RPCPort != prevCfg.Fulcrum.RPCPort {
							changes = append(changes, fmt.Sprintf("fulcrum.rpc_port: %d → %d", prevCfg.Fulcrum.RPCPort, cur.Fulcrum.RPCPort))
						}
						if cur.Fulcrum.SSLEnabled != prevCfg.Fulcrum.SSLEnabled {
							changes = append(changes, fmt.Sprintf("fulcrum.ssl_enabled: %v → %v", prevCfg.Fulcrum.SSLEnabled, cur.Fulcrum.SSLEnabled))
						}
						if cur.Fulcrum.ClearnetAddress != prevCfg.Fulcrum.ClearnetAddress {
							changes = append(changes, "fulcrum.clearnet_address changed")
						}
						if cur.Fulcrum.TorAddress != prevCfg.Fulcrum.TorAddress {
							changes = append(changes, "fulcrum.tor_address changed")
						}
						if cur.Fulcrum.TitleLink != prevCfg.Fulcrum.TitleLink {
							changes = append(changes, "fulcrum.title_link changed")
						}
						if cur.Monero.Enabled != prevCfg.Monero.Enabled {
							changes = append(changes, fmt.Sprintf("monero.enabled: %v → %v", prevCfg.Monero.Enabled, cur.Monero.Enabled))
						}
						if cur.Monero.RPCAddress != prevCfg.Monero.RPCAddress {
							changes = append(changes, "monero.rpc_address changed")
						}
						if cur.Monero.TitleLink != prevCfg.Monero.TitleLink {
							changes = append(changes, "monero.title_link changed")
						}
						if len(changes) > 0 {
							log.Printf("[Config] Applied changes: %s", strings.Join(changes, ", "))
						} else {
							log.Println("[Config] No meaningful changes detected")
						}
					}
					prevCfg = cur
					// Broadcast config change to trigger client reload
					if sseHub != nil {
						sseHub.Broadcast(map[string]interface{}{
							"config_changed": true,
						})
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[Config] Watch error: %v", err)
		}
	}
}

// ==================== Data Types ====================

type Peer struct {
	Addr      string  `json:"addr"`
	Subver    string  `json:"subver"`
	Inbound   bool    `json:"inbound"`
	PingTime  float64 `json:"ping_time"`
	BytesSent int64   `json:"bytes_sent"`
	BytesRecv int64   `json:"bytes_recv"`
}

type BlockInfo struct {
	Height    int     `json:"height"`
	SizeMB    float64 `json:"size_mb"`
	TxCount   int     `json:"tx_count"`
	AgeMins   int     `json:"age_mins"`
	AgeStr    string  `json:"age_str"`
	Timestamp int64   `json:"timestamp"`
}

type BitcoinData struct {
	Connected    bool    `json:"connected"`
	Error        string  `json:"error"`
	SyncProgress float64 `json:"sync_progress"`
	Blocks       int     `json:"blocks"`
	Headers      int     `json:"headers"`
	SizeOnDisk   int64   `json:"size_on_disk"`
	Difficulty   float64 `json:"difficulty"`
	Version      string  `json:"version"`
	OnlyTor      bool    `json:"only_tor"`
	Connections  int     `json:"connections"`
	InboundCount int     `json:"inbound_count"`
	OutboundCount int    `json:"outbound_count"`
	TorCount     int     `json:"tor_count"`
	ClearnetCount int    `json:"clearnet_count"`
	I2PCount     int     `json:"i2p_count"`
	Peers        []Peer  `json:"peers"`
	MempoolSize  int     `json:"mempool_size"`
	MempoolBytes int64   `json:"mempool_bytes"`
	MempoolUsage int64   `json:"mempool_usage"`
	MempoolMax   int64   `json:"mempool_max"`
	MempoolPct   float64 `json:"mempool_pct"`
	Uptime       int64   `json:"uptime"`
	IBD          bool    `json:"ibd"`
	Pruned       bool    `json:"pruned"`
	RecentBlocks []BlockInfo `json:"recent_blocks"`
	ClearnetAddr string  `json:"clearnet_addr"`
	TorAddr      string  `json:"tor_addr"`
	TitleLink    string  `json:"title_link"`
	LatencyMs    int64   `json:"latency_ms"`
}

type MempoolData struct {
	Connected  bool    `json:"connected"`
	Error      string  `json:"error"`
	Fastest    float64 `json:"fastest"`
	HalfHour   float64 `json:"half_hour"`
	Hour       float64 `json:"hour"`
	Economy    float64 `json:"economy"`
	Minimum    float64 `json:"minimum"`
	NextFee    float64 `json:"next_fee"`
	NextTx     int     `json:"next_tx"`
	TxCount    int     `json:"tx_count"`
	VSize      int64   `json:"vsize"`
	TotalFee   int64   `json:"total_fee"`
	EpochPct   float64 `json:"epoch_pct"`
	EpochChg   float64 `json:"epoch_chg"`
	EpochLeft  int     `json:"epoch_left"`
	PriceUSD   float64 `json:"price_usd"`
	MempoolPct float64 `json:"mempool_pct"`
	TitleLink  string  `json:"title_link"`
	Subsat     bool    `json:"subsat"`
	LatencyMs  int64   `json:"latency_ms"`
}

type FulcrumData struct {
	Connected    bool    `json:"connected"`
	Error        string  `json:"error"`
	Version      string  `json:"version"`
	HeaderHeight int     `json:"header_height"`
	BTCHeaders   int     `json:"btc_headers"`
	SyncPct      float64 `json:"sync_pct"`
	ClearnetAddr string  `json:"clearnet_addr"`
	TorAddr      string  `json:"tor_addr"`
	TitleLink    string  `json:"title_link"`
	LatencyMs    int64   `json:"latency_ms"`
}

type MoneroPeer struct {
	Host       string  `json:"host"`
	Port       int     `json:"port"`
	Height     int     `json:"height"`
	Inbound    bool    `json:"inbound"`
	IP         string  `json:"ip"`
	ID         string  `json:"id"`
	LastSeen   int64   `json:"last_seen"`
	RPCPort    int     `json:"rpc_port"`
	AvgDown    int     `json:"avg_down"`
	AvgUp      int     `json:"avg_up"`
	RecvCount  int     `json:"recv_count"`
	SendCount  int     `json:"send_count"`
}

type MoneroData struct {
	Connected    bool           `json:"connected"`
	Error        string         `json:"error"`
	Height       int            `json:"height"`
	TargetHeight int            `json:"target_height"`
	SyncProgress float64        `json:"sync_progress"`
	Version      string         `json:"version"`
	Status       string         `json:"status"`
	InPeers      int            `json:"in_peers"`
	OutPeers     int            `json:"out_peers"`
	DbSize       int64          `json:"db_size"`
	TxPool       int            `json:"tx_pool"`
	NetType      string         `json:"net_type"`
	Uptime       int64          `json:"uptime"`
	TitleLink    string         `json:"title_link"`
	Difficulty   float64        `json:"difficulty"`
	TxCount      int            `json:"tx_count"`
	Connections  int            `json:"connections"`
	Peers        []MoneroPeer   `json:"peers"`
	RecentBlocks []BlockInfo    `json:"recent_blocks"`
	LatencyMs    int64          `json:"latency_ms"`
}

type DashboardData struct {
	Bitcoin       *BitcoinData       `json:"bitcoin"`
	Mempool       *MempoolData       `json:"mempool"`
	Fulcrum       *FulcrumData       `json:"fulcrum"`
	Monero        *MoneroData        `json:"monero"`
	QRCodes       map[string]string  `json:"qr_codes"`
	RefreshInt    int                `json:"refresh_interval"`
	LastSuccess   int64              `json:"last_successful_refresh"`
	Favicon       string             `json:"favicon"`
	FaviconVer    int64              `json:"favicon_ver"`
	ConfigChanged bool               `json:"config_changed"`
	ShowLatency   bool               `json:"show_latency"`
}

// ==================== State ====================

var (
	dataMu      sync.RWMutex
	dashData    *DashboardData
	qrCodes     map[string]string
	lastSuccess int64
	// Server-side block cache: height -> BlockInfo
	blockCache   = make(map[int]BlockInfo)
	blockCacheMu sync.RWMutex
	// Monero block cache
	xmrBlockCache   = make(map[int]BlockInfo)
	xmrBlockCacheMu sync.RWMutex
)

// ==================== Polling ====================

// Track previous error states to avoid repeated logging
var (
	prevErrs = make(map[string]string)
	prevErrsMu sync.RWMutex
)

func logOnce(key, msg string) {
	prevErrsMu.Lock()
	defer prevErrsMu.Unlock()
	if prevErrs[key] != msg {
		log.Printf("[Error] %s: %s", key, msg)
		prevErrs[key] = msg
	}
}

func clearErr(key string) {
	prevErrsMu.Lock()
	defer prevErrsMu.Unlock()
	delete(prevErrs, key)
}

func pollAll() {
	c := getConfig()
	var wg sync.WaitGroup

	var btc *BitcoinData
	var mp *MempoolData
	var ful *FulcrumData
	var xmr *MoneroData

	if c.BitcoinCore.Enabled {
		wg.Add(1)
		go func() { defer wg.Done(); btc = pollBitcoin(c.BitcoinCore) }()
	}
	if c.Mempool.Enabled {
		wg.Add(1)
		go func() { defer wg.Done(); mp = pollMempool(c.Mempool) }()
	}
	if c.Fulcrum.Enabled {
		wg.Add(1)
		go func() { defer wg.Done(); ful = pollFulcrum(c.Fulcrum) }()
	}
	if c.Monero.Enabled {
		wg.Add(1)
		go func() { defer wg.Done(); xmr = pollMonero(c.Monero) }()
	}

	wg.Wait()

	// Log errors once per state change
	if btc != nil && btc.Error != "" {
		logOnce("bitcoin", btc.Error)
	} else {
		clearErr("bitcoin")
	}
	if mp != nil && mp.Error != "" {
		logOnce("mempool", mp.Error)
	} else {
		clearErr("mempool")
	}
	if ful != nil && ful.Error != "" {
		logOnce("fulcrum", ful.Error)
	} else {
		clearErr("fulcrum")
	}
	if xmr != nil && xmr.Error != "" {
		logOnce("monero", xmr.Error)
	} else {
		clearErr("monero")
	}

	// Calculate mempool percentage if we have both bitcoin mempool size and max
	if btc != nil && mp != nil && btc.MempoolMax > 0 {
		mp.MempoolPct = float64(mp.VSize) / float64(btc.MempoolMax) * 100
	}
	// Calculate Fulcrum sync percentage and include BTC headers for display
	if ful != nil {
		if btc != nil {
			ful.BTCHeaders = btc.Headers
			if btc.Headers > 0 {
				ful.SyncPct = float64(ful.HeaderHeight) / float64(btc.Headers) * 100
			}
		}
	}

	now := time.Now().Unix()
	dataMu.Lock()
	dashData = &DashboardData{
		Bitcoin:       btc,
		Mempool:       mp,
		Fulcrum:       ful,
		Monero:        xmr,
		QRCodes:       qrCodes,
		RefreshInt:    c.GlobalRefreshInterval,
		LastSuccess:   now,
		Favicon:       resolveFavicon(c.Favicon),
		FaviconVer:    faviconVer,
		ConfigChanged: false,
		ShowLatency:   c.ShowLatency,
	}
	lastSuccess = now
	dataMu.Unlock()

}

// timedRPC wraps an HTTP call with latency measurement
func timedRPC(fn func() error) int64 {
	start := time.Now()
	fn()
	return time.Since(start).Milliseconds()
}

func rpcCall(url, user, pass, method string, params []interface{}) map[string]interface{} {
	payload := map[string]interface{}{"jsonrpc": "1.0", "id": "nr", "method": method, "params": params}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(body)))
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[RPC] %s: %v", method, err)
		return nil
	}
	defer resp.Body.Close()
	var result struct {
		Result json.RawMessage        `json:"result"`
		Error  interface{}            `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	if result.Error != nil {
		log.Printf("[RPC] %s error: %v", method, result.Error)
		return nil
	}
	// Try to decode as object first
	var obj map[string]interface{}
	if err := json.Unmarshal(result.Result, &obj); err == nil {
		return obj
	}
	// If it's a raw value (like uptime which returns a number), decode it
	var raw interface{}
	if err := json.Unmarshal(result.Result, &raw); err == nil {
		return map[string]interface{}{"result": raw}
	}
	return nil
}

// rpcCallTimed returns both the result and the latency in ms
func rpcCallTimed(url, user, pass, method string, params []interface{}) (map[string]interface{}, int64) {
	start := time.Now()
	result := rpcCall(url, user, pass, method, params)
	latency := time.Since(start).Milliseconds()
	return result, latency
}

func getJSON(url string, v interface{}) error {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// getJSONTimed returns both the error and the latency in ms
func getJSONTimed(url string, v interface{}) int64 {
	start := time.Now()
	err := getJSON(url, v)
	_ = err // caller checks result separately
	return time.Since(start).Milliseconds()
}

func pollBitcoin(c BitcoinCfg) *BitcoinData {
	start := time.Now()
	rpcURL := strings.TrimRight(c.RPCAddress, "/")
	bc := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "getblockchaininfo", nil)
	if bc == nil {
		return &BitcoinData{Error: "Cannot connect to Bitcoin Core", LatencyMs: time.Since(start).Milliseconds()}
	}
	ni := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "getnetworkinfo", nil)
	mi := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "getmempoolinfo", nil)
	pi := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "getpeerinfo", nil)
	ui := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "uptime", nil)

	var peers []Peer
	var inC, outC, torC, clearC, i2pC int
	if pi != nil {
		if pl, ok := pi["result"].([]interface{}); ok {
			for _, p := range pl {
				pm := p.(map[string]interface{})
				addr := fmt.Sprintf("%v", pm["addr"])
				isIn, _ := pm["inbound"].(bool)
				if isIn {
					inC++
				} else {
					outC++
				}
				if strings.Contains(addr, ".onion") {
					torC++
				} else if strings.Contains(addr, ".i2p") {
					i2pC++
				} else {
					clearC++
				}
				pt := 0.0
				if v, ok := pm["pingtime"].(float64); ok {
					pt = v * 1000
				}
				peers = append(peers, Peer{
					Addr: addr, Subver: fmt.Sprintf("%v", pm["subver"]),
					Inbound: isIn, PingTime: pt,
					BytesSent: int64(pm["bytessent"].(float64)),
					BytesRecv: int64(pm["bytesrecv"].(float64)),
				})
			}
		}
	}

	ver := "Unknown"
	if ni != nil {
		if sv, ok := ni["subversion"].(string); ok {
			v := strings.TrimPrefix(strings.TrimSuffix(sv, "/"), "/Satoshi:")
			if v != "" {
				ver = "v" + v
			}
		}
	}

	var uptime int64
	if ui != nil {
		if v, ok := ui["result"].(float64); ok {
			uptime = int64(v)
		}
	}

	memSize := 0
	memBytes := int64(0)
	memUsage := int64(0)
	maxMempool := int64(0)
	if mi != nil {
		// rpcCall already returns the result object directly
		if v, ok := mi["size"]; ok {
			memSize = int(v.(float64))
		}
		if v, ok := mi["bytes"]; ok {
			memBytes = int64(v.(float64))
		}
		if v, ok := mi["usage"]; ok {
			memUsage = int64(v.(float64))
		}
		if v, ok := mi["maxmempool"]; ok {
			maxMempool = int64(v.(float64))
		}
	}

	sp := 0.0
	if v, ok := bc["verificationprogress"].(float64); ok {
		sp = v * 100
	}

	memPct := 0.0
	if maxMempool > 0 {
		memPct = float64(memUsage) / float64(maxMempool) * 100
	}

	ibd := false
	if v, ok := bc["initialblockdownload"].(bool); ok {
		ibd = v
	}
	pruned := false
	if v, ok := bc["pruned"].(bool); ok {
		pruned = v
	}

	// Fetch recent blocks for the blockchain graphic (configurable, default 15)
	// Uses server-side cache: only fetch new blocks, reuse cached data for existing ones
	var recentBlocks []BlockInfo
	currentHeight := int(bc["blocks"].(float64))
	now := time.Now()
	blockCount := c.RecentBlocksCount

	blockCacheMu.Lock()
	// Calculate valid window
	minH := currentHeight - blockCount + 1
	if minH < 0 {
		minH = 0
	}
	maxH := currentHeight

	// Remove entries outside current window
	for h := range blockCache {
		if h < minH || h > maxH {
			delete(blockCache, h)
		}
	}

	// Count how many we need to fetch
	fetchCount := 0
	for i := 0; i < blockCount; i++ {
		h := currentHeight - i
		if h < 0 {
			break
		}
		if _, ok := blockCache[h]; !ok {
			fetchCount++
		}
	}

	// Fetch missing blocks
	for i := 0; i < blockCount; i++ {
		h := currentHeight - i
		if h < 0 {
			break
		}
		if _, ok := blockCache[h]; ok {
			continue
		}
		// Fetch from RPC
		hashResult := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "getblockhash", []interface{}{h})
		if hashResult == nil {
			continue
		}
		hashRaw, ok := hashResult["result"]
		if !ok {
			continue
		}
		hash, ok := hashRaw.(string)
		if !ok {
			continue
		}
		block := rpcCall(rpcURL, c.RPCUser, c.RPCPassword, "getblock", []interface{}{hash, 2})
		if block == nil {
			continue
		}
		txCount := 0
		if txs, ok := block["tx"].([]interface{}); ok {
			txCount = len(txs)
		}
		sizeBytes := 0.0
		if sz, ok := block["size"].(float64); ok {
			sizeBytes = sz
		}
		sizeMB := sizeBytes / (1024 * 1024)
		blockTime := int64(0)
		if bt, ok := block["time"].(float64); ok {
			blockTime = int64(bt)
		}
		blockCache[h] = BlockInfo{
			Height:    h,
			SizeMB:    sizeMB,
			TxCount:   txCount,
			Timestamp: blockTime,
		}
	}

	// Build result list with updated ages
	cachedCount := 0
	for i := 0; i < blockCount; i++ {
		h := currentHeight - i
		if h < 0 {
			break
		}
		if cached, ok := blockCache[h]; ok {
			diff := now.Sub(time.Unix(cached.Timestamp, 0))
			ageMins := int(diff.Minutes())
			if ageMins < 1 {
				cached.AgeStr = "Just now"
			} else if ageMins < 60 {
				cached.AgeStr = fmt.Sprintf("%d min ago", ageMins)
			} else {
				cached.AgeStr = fmt.Sprintf("%dh ago", int(diff.Hours()))
			}
			cached.AgeMins = ageMins
			blockCache[h] = cached
			recentBlocks = append(recentBlocks, cached)
			cachedCount++
		}
	}
	blockCacheMu.Unlock()

	// Log only on first load or when new blocks are fetched
	if fetchCount > 0 {
		log.Printf("[BTC] %d recent blocks (%d fetched, %d cached)", len(recentBlocks), fetchCount, cachedCount-fetchCount)
	}

	return &BitcoinData{
		Connected: true, SyncProgress: sp,
		Blocks: int(bc["blocks"].(float64)), Headers: int(bc["headers"].(float64)),
		SizeOnDisk: int64(bc["size_on_disk"].(float64)),
		Difficulty: bc["difficulty"].(float64), Version: ver,
		OnlyTor: torC > 0 && clearC == 0,
		Connections: len(peers), InboundCount: inC, OutboundCount: outC,
		TorCount: torC, ClearnetCount: clearC, I2PCount: i2pC,
		Peers: peers, MempoolSize: memSize, MempoolBytes: memBytes,
		MempoolUsage: memUsage, MempoolMax: maxMempool, MempoolPct: memPct,
		Uptime: uptime, IBD: ibd, Pruned: pruned, RecentBlocks: recentBlocks,
		ClearnetAddr: c.ClearnetAddress, TorAddr: c.TorAddress,
		TitleLink: c.TitleLink,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

func pollMempool(c MempoolCfg) *MempoolData {
	start := time.Now()
	api := strings.TrimRight(c.APIEndpoint, "/")
	
	// Use /api/v1/fees/precise for subsat, /api/v1/fees/recommended for normal
	var fees map[string]float64
	var feesURL string
	if c.Subsat {
		feesURL = api + "/v1/fees/precise"
	} else {
		feesURL = api + "/v1/fees/recommended"
	}
	if err := getJSON(feesURL, &fees); err != nil {
		return &MempoolData{Error: err.Error(), LatencyMs: time.Since(start).Milliseconds()}
	}

	var blocks []map[string]interface{}
	getJSON(api+"/v1/fees/mempool-blocks", &blocks)

	var mem map[string]interface{}
	getJSON(api+"/mempool", &mem)

	var diff map[string]interface{}
	getJSON(api+"/v1/difficulty-adjustment", &diff)

	var price map[string]float64
	getJSON(api+"/v1/prices", &price)

	var nextFee float64
	var nextTx int
	if len(blocks) > 0 {
		nextFee = blocks[0]["medianFee"].(float64)
		nextTx = int(blocks[0]["nTx"].(float64))
	}

	txCount := 0
	var vSize int64
	var totalFee int64
	if mem != nil {
		txCount = int(mem["count"].(float64))
		vSize = int64(mem["vsize"].(float64))
		totalFee = int64(mem["total_fee"].(float64))
	}

	epochPct := 0.0
	epochChg := 0.0
	epochLeft := 0
	if diff != nil {
		epochPct = diff["progressPercent"].(float64)
		epochChg = diff["difficultyChange"].(float64)
		epochLeft = int(diff["remainingBlocks"].(float64))
	}

	priceUSD := 0.0
	if price != nil {
		priceUSD = price["USD"]
	}

	return &MempoolData{
		Connected: true,
		Fastest: fees["fastestFee"], HalfHour: fees["halfHourFee"],
		Hour: fees["hourFee"], Economy: fees["economyFee"], Minimum: fees["minimumFee"],
		NextFee: nextFee, NextTx: nextTx,
		TxCount: txCount, VSize: vSize, TotalFee: totalFee,
		EpochPct: epochPct, EpochChg: epochChg, EpochLeft: epochLeft,
		PriceUSD: priceUSD, TitleLink: c.TitleLink,
		Subsat: c.Subsat,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

func pollFulcrum(c FulcrumCfg) *FulcrumData {
	start := time.Now()
	addr := fmt.Sprintf("%s:%d", c.RPCAddress, c.RPCPort)
	
	var conn net.Conn
	var err error
	
	if c.SSLEnabled {
		conn, err = tls.Dial("tcp", addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		conn, err = net.Dial("tcp", addr)
	}
	if err != nil {
		return &FulcrumData{
			Error: fmt.Sprintf("Cannot connect to Fulcrum: %v", err),
			ClearnetAddr: c.ClearnetAddress, TorAddr: c.TorAddress,
			TitleLink: c.TitleLink,
			LatencyMs: time.Since(start).Milliseconds(),
		}
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	
	rpcCall := func(method string, params []interface{}) map[string]interface{} {
		payload := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  method,
			"params":  params,
		}
		body, _ := json.Marshal(payload)
		fmt.Fprintf(conn, "%s\n", body)
		
		reader := bufio.NewReader(conn)
		line, err := reader.ReadString('\n')
		if err != nil {
			log.Printf("[Fulcrum] RPC error (%s): %v", method, err)
			return nil
		}
		var result struct {
			Result interface{} `json:"result"`
			Error  interface{} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			return nil
		}
		if result.Error != nil {
			log.Printf("[Fulcrum] RPC error (%s): %v", method, result.Error)
			return nil
		}
		if m, ok := result.Result.(map[string]interface{}); ok {
			return m
		}
		if s, ok := result.Result.([]interface{}); ok && len(s) > 0 {
			if m, ok := s[0].(map[string]interface{}); ok {
				return m
			}
			return map[string]interface{}{"result": s[0]}
		}
		return map[string]interface{}{"result": result.Result}
	}
	
	verResult := rpcCall("server.version", []interface{}{"NodeRouter", "1.4"})
	headerResult := rpcCall("blockchain.headers.subscribe", []interface{}{})
	
	version := ""
	if verResult != nil {
		if v, ok := verResult["result"].(string); ok {
			version = v
		} else if v, ok := verResult["0"].(string); ok {
			version = v
		}
	}
	
	headerHeight := 0
	if headerResult != nil {
		if h, ok := headerResult["height"].(float64); ok {
			headerHeight = int(h)
		}
	}
	
	return &FulcrumData{
		Connected:    true,
		Version:      version,
		HeaderHeight: headerHeight,
		ClearnetAddr: c.ClearnetAddress,
		TorAddr:      c.TorAddress,
		TitleLink:    c.TitleLink,
		LatencyMs:    time.Since(start).Milliseconds(),
	}
}

func pollMonero(c MoneroCfg) *MoneroData {
	start := time.Now()
	rpcURL := strings.TrimRight(c.RPCAddress, "/")
	// Use REST endpoint /get_info for node info
	var info map[string]interface{}
	if err := getJSON(rpcURL+"/get_info", &info); err != nil {
		return &MoneroData{Error: "Cannot connect to Monero", LatencyMs: time.Since(start).Milliseconds()}
	}

	h := 0
	if v, ok := info["height"]; ok {
		h = int(v.(float64))
	}
	th := 0
	if v, ok := info["target_height"]; ok {
		th = int(v.(float64))
	}
	if th == 0 || th <= h {
		th = h
	}
	sp := float64(h) / float64(th) * 100

	// Get version via JSON-RPC /json_rpc with method "get_version"
	ver := "Unknown"
	verPayload := map[string]interface{}{"jsonrpc": "2.0", "id": "0", "method": "get_version"}
	verBody, _ := json.Marshal(verPayload)
	verReq, _ := http.NewRequest("POST", rpcURL+"/json_rpc", strings.NewReader(string(verBody)))
	verReq.Header.Set("Content-Type", "application/json")
	verClient := &http.Client{Timeout: 10 * time.Second}
	verResp, err := verClient.Do(verReq)
	if err == nil {
		defer verResp.Body.Close()
		var verResult struct {
			Result map[string]interface{} `json:"result"`
		}
		if err := json.NewDecoder(verResp.Body).Decode(&verResult); err == nil && verResult.Result != nil {
			if v, ok := verResult.Result["version"]; ok {
				verNum := uint32(v.(float64))
				if verNum > 0 {
					major := (verNum >> 16) & 0xFF
					minor := (verNum >> 8) & 0xFF
					patch := verNum & 0xFF
					ver = fmt.Sprintf("%d.%d.%d", major, minor, patch)
				}
			}
		}
	}
	status := "OK"
	if s, ok := info["status"]; ok && s != nil {
		status = fmt.Sprintf("%v", s)
	}
	inPeers := 0
	if v, ok := info["incoming_connections_count"]; ok && v != nil {
		inPeers = int(v.(float64))
	}
	outPeers := 0
	if v, ok := info["outgoing_connections_count"]; ok && v != nil {
		outPeers = int(v.(float64))
	}
	dbSize := int64(0)
	if v, ok := info["database_size"]; ok && v != nil {
		dbSize = int64(v.(float64))
	}
	txPool := 0
	if v, ok := info["tx_pool_size"]; ok && v != nil {
		txPool = int(v.(float64))
	}
	netType := "mainnet"
	if v, ok := info["nettype"]; ok && v != nil {
		netType = fmt.Sprintf("%v", v)
	}
	// Calculate uptime from start_time
	var uptime int64
	if v, ok := info["start_time"]; ok && v != nil {
		startTime := int64(v.(float64))
		if startTime > 0 {
			uptime = time.Now().Unix() - startTime
		}
	}

	// Get difficulty and tx_count from /get_info
	difficulty := 0.0
	if v, ok := info["difficulty"]; ok && v != nil {
		difficulty = v.(float64)
	}
	txCount := 0
	if v, ok := info["tx_count"]; ok && v != nil {
		txCount = int(v.(float64))
	}

	// Fetch peers via non-JSON RPC endpoint
	var peers []MoneroPeer
	peerResp, err := http.Get(rpcURL + "/get_connections")
	if err == nil && peerResp != nil {
		defer peerResp.Body.Close()
		var peerResult struct {
			Connections []map[string]interface{} `json:"connections"`
			Status      string                   `json:"status"`
		}
		if err := json.NewDecoder(peerResp.Body).Decode(&peerResult); err == nil && peerResult.Status == "OK" {
			for _, p := range peerResult.Connections {
				peer := MoneroPeer{
					Host: fmt.Sprintf("%v", p["host"]),
					Port: int(p["port"].(float64)),
					Height: int(p["height"].(float64)),
					Inbound: p["incoming"].(bool),
					IP: fmt.Sprintf("%v", p["ip"]),
					ID: fmt.Sprintf("%v", p["peer_id"]),
					LastSeen: int64(p["last_seen"].(float64)),
					RPCPort: int(p["rpc_port"].(float64)),
					AvgDown: int(p["avg_download"].(float64)),
					AvgUp: int(p["avg_upload"].(float64)),
					RecvCount: int(p["recv_count"].(float64)),
					SendCount: int(p["send_count"].(float64)),
				}
				peers = append(peers, peer)
			}
		}
	}

	// Fetch recent blocks with caching
	// Note: get_info height is the NEXT block height, so latest block is at h-1
	blockCount := c.RecentBlocksCount
	if blockCount < 6 {
		blockCount = 15
	}
	if blockCount > 30 {
		blockCount = 30
	}
	var recentBlocks []BlockInfo
	now := time.Now()

	// Latest actual block height
	latestH := h - 1
	if latestH < 0 {
		latestH = 0
	}

	xmrBlockCacheMu.Lock()
	// Always prune cache to current window
	minH := latestH - blockCount + 1
	if minH < 0 { minH = 0 }
	maxH := latestH
	for bh := range xmrBlockCache {
		if bh < minH || bh > maxH {
			delete(xmrBlockCache, bh)
		}
	}

	// Always fetch the full range to ensure we have the latest data
	// (Monero's get_block_headers_range is efficient - single RPC call)
	endH := latestH
	startH := minH
	if startH > endH { startH = endH }
	if startH < 0 { startH = 0 }

	headersPayload := map[string]interface{}{"jsonrpc": "2.0", "id": "0", "method": "get_block_headers_range", "params": map[string]interface{}{"start_height": startH, "end_height": endH}}
	headersBody, _ := json.Marshal(headersPayload)
	headersReq, _ := http.NewRequest("POST", rpcURL+"/json_rpc", strings.NewReader(string(headersBody)))
	headersReq.Header.Set("Content-Type", "application/json")
	headersClient := &http.Client{Timeout: 15 * time.Second}
	headersResp, err := headersClient.Do(headersReq)
	if err == nil {
		defer headersResp.Body.Close()
		var headersResult struct {
			Result struct {
				Headers []map[string]interface{} `json:"headers"`
			} `json:"result"`
		}
		if err := json.NewDecoder(headersResp.Body).Decode(&headersResult); err == nil {
			// Clear old cache entries and replace with fresh data
			for bh := range xmrBlockCache {
				delete(xmrBlockCache, bh)
			}
			for _, hdr := range headersResult.Result.Headers {
				height := int(hdr["height"].(float64))
				ts := int64(hdr["timestamp"].(float64))
				sizeMB := hdr["block_weight"].(float64) / (1024 * 1024)
				txCount := int(hdr["num_txes"].(float64))
				diff := now.Sub(time.Unix(ts, 0))
				ageMins := int(diff.Minutes())
				ageStr := "--"
				if ageMins < 1 {
					ageStr = "Just now"
				} else if ageMins < 60 {
					ageStr = fmt.Sprintf("%d min ago", ageMins)
				} else {
					ageStr = fmt.Sprintf("%dh ago", int(diff.Hours()))
				}
				xmrBlockCache[height] = BlockInfo{
					Height: height, SizeMB: sizeMB, TxCount: txCount,
					AgeMins: ageMins, AgeStr: ageStr, Timestamp: ts,
				}
			}
		}
	}

	// Build result list with updated ages
	for i := 0; i < blockCount; i++ {
		bh := latestH - i
		if bh < 0 { break }
		if cached, ok := xmrBlockCache[bh]; ok {
			diff := now.Sub(time.Unix(cached.Timestamp, 0))
			ageMins := int(diff.Minutes())
			if ageMins < 1 {
				cached.AgeStr = "Just now"
			} else if ageMins < 60 {
				cached.AgeStr = fmt.Sprintf("%d min ago", ageMins)
			} else {
				cached.AgeStr = fmt.Sprintf("%dh ago", int(diff.Hours()))
			}
			cached.AgeMins = ageMins
			xmrBlockCache[bh] = cached
			recentBlocks = append(recentBlocks, cached)
		}
	}
	xmrBlockCacheMu.Unlock()

	return &MoneroData{
		Connected: true, Height: h, TargetHeight: th, SyncProgress: sp,
		Version: ver, Status: status,
		InPeers: inPeers, OutPeers: outPeers,
		DbSize: dbSize, TxPool: txPool,
		NetType: netType, Uptime: uptime,
		TitleLink: c.TitleLink,
		Difficulty: difficulty, TxCount: txCount,
		Connections: len(peers), Peers: peers,
		RecentBlocks: recentBlocks,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// ==================== QR Codes ====================

func genQR(data string) string {
	if data == "" {
		return ""
	}
	png, err := qrcode.Encode(data, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("data:image/png;base64,%s", base64.StdEncoding.EncodeToString(png))
}

func genAllQRs(c *Config) map[string]string {
	m := make(map[string]string)
	if v := genQR(c.BitcoinCore.TorAddress); v != "" {
		m["btcTor"] = v
	}
	if v := genQR(c.BitcoinCore.ClearnetAddress); v != "" {
		m["btcClear"] = v
	}
	if v := genQR(c.Fulcrum.TorAddress); v != "" {
		m["fulTor"] = v
	}
	if v := genQR(c.Fulcrum.ClearnetAddress); v != "" {
		m["fulClear"] = v
	}
	log.Printf("[QR] Generated %d codes", len(m))
	return m
}

// ==================== SSE Hub ====================

type Hub struct {
	clients    map[chan []byte]bool
	mu         sync.RWMutex
	broadcast  chan []byte
	register   chan chan []byte
	unregister chan chan []byte
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan []byte]bool),
		broadcast: make(chan []byte, 256),
		register: make(chan chan []byte),
		unregister: make(chan chan []byte),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[SSE] Connected (%d total)", len(h.clients))
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client)
			}
			h.mu.Unlock()
			log.Printf("[SSE] Disconnected (%d total)", len(h.clients))
		case msg := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client <- msg:
				default:
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Broadcast(data interface{}) {
	msg, err := json.Marshal(data)
	if err != nil {
		return
	}
	h.broadcast <- []byte(fmt.Sprintf("data: %s\n\n", msg))
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "No streaming", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	client := make(chan []byte, 256)
	h.register <- client
	defer func() { h.unregister <- client }()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-client:
			fmt.Fprint(w, string(msg))
			flusher.Flush()
		}
	}
}

// ==================== Helpers ====================

func formatBytes(b int64) string {
	if b == 0 {
		return "0 B"
	}
	k := 1024.0
	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	i := int(math.Floor(math.Log(float64(b)) / math.Log(k)))
	if i >= len(sizes) {
		i = len(sizes) - 1
	}
	return fmt.Sprintf("%.2f %s", float64(b)/math.Pow(k, float64(i)), sizes[i])
}

// formatBytesDecimal formats bytes using decimal (SI) units.
// Used for mempool maxmempool display where 2,000,000,000 = 2.00 GB.
func formatBytesDecimal(b int64) string {
	if b == 0 {
		return "0 B"
	}
	k := 1000.0
	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	i := int(math.Floor(math.Log(float64(b)) / math.Log(k)))
	if i >= len(sizes) {
		i = len(sizes) - 1
	}
	return fmt.Sprintf("%.2f %s", float64(b)/math.Pow(k, float64(i)), sizes[i])
}

func formatNum(n int) string {
	return fmt.Sprintf("%d", n)
}

func formatUptime(s int64) string {
	if s == 0 {
		return "--"
	}
	d := s / 86400
	h := (s % 86400) / 3600
	m := (s % 3600) / 60
	if d > 0 {
		return fmt.Sprintf("%dd %dh %dm", d, h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func syncClass(pct float64) string {
	if pct < 80 {
		return "sync-low"
	}
	if pct < 95 {
		return "sync-mid"
	}
	return "sync-high"
}

// mempoolClass returns CSS class for mempool fill bar.
// Green when empty, turns orange at 50%, red at 80%+.
func mempoolClass(pct float64) string {
	if pct >= 80 {
		return "sync-low" // red
	}
	if pct >= 50 {
		return "sync-mid" // orange
	}
	return "sync-high" // green
}

// ==================== Main ====================

var sseHub *Hub
var tpl *template.Template

func main() {
	flag.StringVar(&cfgPath, "config", "config.yaml", "Config path")
	port := flag.String("port", "5000", "HTTP port")
	flag.Parse()

	if p := os.Getenv("NODEROUTER_PORT"); p != "" {
		*port = p
	}
	if p := os.Getenv("NODEROUTER_CONFIG"); p != "" {
		cfgPath = p
	}

	log.SetFlags(log.Ldate | log.Ltime)
	log.Println("==================================================")
	log.Println("NodeRouter (Go) starting...")
	log.Println("==================================================")

	if err := loadConfig(); err != nil {
		log.Fatalf("[Main] Config error: %v", err)
	}

	pollAll()

	sseHub = NewHub()
	go sseHub.Run()

	go func() {
		for {
			interval := time.Duration(getConfig().GlobalRefreshInterval) * time.Second
			ticker := time.NewTicker(interval)
			<-ticker.C
			ticker.Stop()

			pollAll()
			dataMu.RLock()
			d := *dashData
			dataMu.RUnlock()

			sseHub.Broadcast(map[string]interface{}{
				"bitcoin":               d.Bitcoin,
				"fulcrum":               d.Fulcrum,
				"mempool":               d.Mempool,
				"monero":                d.Monero,
				"last_successful_refresh": d.LastSuccess,
				"refresh_interval":      d.RefreshInt,
			})
		}
	}()

	go watchConfig()

	funcs := template.FuncMap{
		"formatBytes":       formatBytes,
		"formatBytesDec":    formatBytesDecimal,
		"ceilFloat": func(f float64) int {
			return int(math.Ceil(f))
		},
		"formatLastTime": func(ts int64) string {
			if ts == 0 {
				return "--"
			}
			return time.Unix(ts, 0).Format("15:04:05")
		},
		"formatNum":    formatNum,
		"formatUptime": formatUptime,
		"syncClass":    syncClass,
		"mempoolClass": mempoolClass,
		"hasQR": func(id string) bool {
			dataMu.RLock()
			_, ok := qrCodes[id]
			dataMu.RUnlock()
			return ok
		},
		"getQR": func(id string) template.URL {
			dataMu.RLock()
			v := qrCodes[id]
			dataMu.RUnlock()
			return template.URL(v)
		},
		"int": func(f float64) int {
			return int(f)
		},
		"divFloat64": func(a, b int64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},
		"divFloat": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return float64(a) / float64(b)
		},
		"commaInt": func(n int) string {
			s := fmt.Sprintf("%d", n)
			if n < 0 {
				s = s[1:]
			}
			for i := len(s) - 3; i > 0; i -= 3 {
				s = s[:i] + "," + s[i:]
			}
			if n < 0 {
				s = "-" + s
			}
			return s
		},
		"commaFloat": func(n int) string {
			s := fmt.Sprintf("%d", n)
			if n < 0 {
				s = s[1:]
			}
			for i := len(s) - 3; i > 0; i -= 3 {
				s = s[:i] + "," + s[i:]
			}
			if n < 0 {
				s = "-" + s
			}
			return s
		},
		"mulFloat": func(a int, b float64) float64 {
			return float64(a) * b
		},
	}
	tpl = template.Must(template.New("").Funcs(funcs).ParseFS(embeddedFiles, "templates/index.html"))

	staticFS, _ := fs.Sub(embeddedFiles, "static")
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Favicon route - proxy remote favicons or serve local ones
	http.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		c := getConfig()
		faviconURL := resolveFavicon(c.Favicon)
		// Remote URL - proxy it
		if strings.HasPrefix(faviconURL, "http://") || strings.HasPrefix(faviconURL, "https://") {
			resp, err := http.Get(faviconURL)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			io.Copy(w, resp.Body)
			return
		}
		// Local file - serve from embedded FS
		data, err := fs.ReadFile(embeddedFiles, "static"+faviconURL)
		if err != nil {
			// Fallback to bitcoin logo
			data, err = fs.ReadFile(embeddedFiles, "static/logos/bitcoin.svg")
			if err != nil {
				http.NotFound(w, r)
				return
			}
		}
		// Set content type based on extension
		if strings.HasSuffix(faviconURL, ".png") {
			w.Header().Set("Content-Type", "image/png")
		} else if strings.HasSuffix(faviconURL, ".svg") {
			w.Header().Set("Content-Type", "image/svg+xml")
		} else if strings.HasSuffix(faviconURL, ".ico") {
			w.Header().Set("Content-Type", "image/x-icon")
		}
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Write(data)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		dataMu.RLock()
		d := *dashData
		dataMu.RUnlock()
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		if err := tpl.ExecuteTemplate(w, "index.html", d); err != nil {
			log.Printf("[HTTP] Template error: %v", err)
			http.Error(w, "Template error", 500)
		}
	})

	http.HandleFunc("/sse/stream", sseHub.ServeHTTP)

	log.Printf("[Main] Server on 0.0.0.0:%s", *port)
	log.Printf("[Main] Dashboard at http://localhost:%s", *port)
	log.Println("[Main] Ready")

	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("[Main] %v", err)
	}
}
