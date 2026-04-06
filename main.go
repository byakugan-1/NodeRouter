package main

import (
	"bufio"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/hex"
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
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
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
	Notifications         NotifCfg   `yaml:"notifications"`
	Auth                  AuthCfg    `yaml:"auth"`
	Tor                   TorCfg     `yaml:"tor"`
}

type AuthCfg struct {
	Mode             string `yaml:"mode"`               // "none" | "password" | "auth47" | "both"
	Password         string `yaml:"password"`           // Plaintext password (hashed on first load)
	PasswordHash     string `yaml:"password_hash"`      // bcrypt hash (auto-generated)
	AdminPaymentCode string `yaml:"admin_payment_code"` // Single allowed BIP47 payment code
	SessionExpiry    int    `yaml:"session_expiry"`     // Session expiry in hours (default 24)
}

type TorCfg struct {
	Enabled bool `yaml:"enabled"` // Enable Tor hidden service
}

type NotifCfg struct {
	GotifyURL   string `yaml:"gotify_url"`
	GotifyToken string `yaml:"gotify_token"`
	Enabled     bool   `yaml:"enabled"`
	CheckFreq   int    `yaml:"check_freq"`
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
	sseHub        *Hub
	notifMu       sync.RWMutex
	notifSettings *NotifSettings
	lastBlockHeight int
)

// txIDRegex validates hex TXIDs (64 hex chars)
var txIDRegex = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// NotifSettings holds ephemeral notification settings (not saved to config)
type NotifSettings struct {
	FeeEnabled          bool           `json:"fee_enabled"`
	FeeThreshold        float64        `json:"fee_threshold"`
	FeeAboveThreshold   bool           `json:"fee_above_threshold"`
	FeeType             string         `json:"fee_type"`
	FeeNotified         bool           `json:"fee_notified"`
	NewBlockEnabled     bool           `json:"new_block_enabled"`
	SpecificBlockEnabled bool          `json:"specific_block_enabled"`
	SpecificBlockHeight int            `json:"specific_block_height"`
	SpecificBlockNotified bool         `json:"specific_block_notified"`
	TxWatches           []TxWatchEntry `json:"tx_watches"`
}

type TxWatchEntry struct {
	TxID          string `json:"txid"`
	Notified      bool   `json:"notified"`
	Confirmations int    `json:"confirmations"`
	TargetConfs   int    `json:"target_confs"`
}

type NotifSettingsResponse struct {
	RefreshInterval int    `json:"refresh_interval"`
	ShowLatency     bool   `json:"show_latency"`
	BtcBlocksCount  int    `json:"btc_blocks_count"`
	XmrBlocksCount  int    `json:"xmr_blocks_count"`
	ConnBtcRpc      string `json:"conn_btc_rpc"`
	ConnBtcUser     string `json:"conn_btc_user"`
	ConnBtcPass     string `json:"conn_btc_pass"`
	SvcMpEnabled    bool   `json:"svc_mp_enabled"`
	ConnMpApi       string `json:"conn_mp_api"`
	SvcFulEnabled   bool   `json:"svc_ful_enabled"`
	ConnFulcrum     string `json:"conn_fulcrum"`
	SvcXmrEnabled   bool   `json:"svc_xmr_enabled"`
	ConnXmrRpc      string `json:"conn_xmr_rpc"`
	GotifyURL       string `json:"gotify_url"`
	GotifyToken     string `json:"gotify_token"`
	GotifyConfigured bool  `json:"gotify_configured"`
	NotifEnabled    bool   `json:"notif_enabled"`
	CheckFreq       int    `json:"check_freq"`
	FeeNotifEnabled bool   `json:"fee_notif_enabled"`
	FeeThreshold    float64 `json:"fee_threshold"`
	FeeAboveThreshold bool `json:"fee_above_threshold"`
	NewBlockNotif   bool   `json:"new_block_notif"`
	SpecificBlockNotif bool `json:"specific_block_notif"`
	SpecificBlockHeight int `json:"specific_block_height"`
	TxWatches       []TxWatchEntry `json:"tx_watches"`
	TxTargetConfs   int    `json:"tx_target_confs"`
}

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
	// Validate auth mode
	validAuthModes := map[string]bool{"": true, "none": true, "password": true, "auth47": true, "both": true}
	if !validAuthModes[c.Auth.Mode] {
		c.Auth.Mode = "none"
	}
	// If Tor is disabled, override auth47 to disabled
	if !c.Tor.Enabled && (c.Auth.Mode == "auth47" || c.Auth.Mode == "both") {
		if c.Auth.Mode == "both" {
			c.Auth.Mode = "password"
			log.Println("[Config] Tor disabled: Auth47 disabled, falling back to password-only auth")
		} else {
			c.Auth.Mode = "none"
			log.Println("[Config] Tor disabled: Auth47 disabled, no authentication enabled")
		}
	}
	// Auto-hash plaintext password if provided
	if c.Auth.Password != "" && c.Auth.PasswordHash == "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(c.Auth.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("[Auth] Failed to hash password: %v", err)
		} else {
			c.Auth.PasswordHash = string(hash)
			c.Auth.Password = "" // Clear plaintext
			// Write back to config
			data, _ := yaml.Marshal(c)
			os.WriteFile(cfgPath, data, 0644)
			log.Println("[Auth] Password hashed and saved to config")
		}
	}
	cfgMu.Lock()
	cfg = c
	faviconVer = time.Now().Unix() // increment version for cache-busting
	cfgMu.Unlock()
	log.Printf("[Config] Loaded from %s (interval: %ds, auth: %s, tor: %v)", cfgPath, c.GlobalRefreshInterval, c.Auth.Mode, c.Tor.Enabled)
	return nil
}

// writeConfig writes the current config to the config file
func writeConfig() error {
	cfgMu.RLock()
	c := cfg
	cfgMu.RUnlock()
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
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
	NotifEnabled  bool               `json:"notif_enabled"`
	RefreshStart  int64              `json:"refresh_start"`
	TorHostname   string             `json:"tor_hostname"`
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
	// Auth session store
	authSessions   = make(map[string]*Session)
	authSessionsMu sync.RWMutex
	// Auth47 pending authentications
	auth47Pending   = make(map[string]*Auth47Pending)
	auth47PendingMu sync.RWMutex
	// JWT secret (generated on startup)
	jwtSecret []byte
	// Tor hostname (populated at startup)
	torHostname string
	torHostnameMu sync.RWMutex
	// Rate limiting for auth endpoints
	authRateLimit   = make(map[string]int64)
	authRateLimitMu sync.RWMutex
)

// Session represents an authenticated user session
type Session struct {
	LoggedIn   bool      `json:"logged_in"`
	AuthMethod string    `json:"auth_method"` // "password" | "auth47"
	Paynym     string    `json:"paynym,omitempty"`
	PaynymName string    `json:"paynym_name,omitempty"`
	PaynymAvatar string  `json:"paynym_avatar,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Auth47Pending tracks pending Auth47 authentication
type Auth47Pending struct {
	Nonce     string
	CreatedAt time.Time
	Verified  bool
	Paynym    string
}

// Auth47URI response
type Auth47URIResponse struct {
	Nonce       string `json:"nonce"`
	URI         string `json:"uri"`
	PaymentCode string `json:"payment_code,omitempty"`
	Expires     int64  `json:"expires"`
}

// LoginRequest for password authentication
type LoginRequest struct {
	Password string `json:"password"`
}

// LoginResponse
type LoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Token   string `json:"token,omitempty"`
}

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
	// Calculate refresh start time (last refresh time + interval)
	refreshStart := now - int64(c.GlobalRefreshInterval)
	
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
		NotifEnabled:  c.Notifications.Enabled && c.Notifications.GotifyURL != "" && c.Notifications.GotifyToken != "",
		RefreshStart:  refreshStart,
		TorHostname:   getTorHostname(),
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
	
	// Auto-append /api if not present
	if !strings.HasSuffix(api, "/api") {
		api = api + "/api"
	}
	
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

// ==================== Authentication ====================

// generateJWTSecret creates a random 32-byte secret for JWT signing
func generateJWTSecret() []byte {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatalf("[Auth] Failed to generate JWT secret: %v", err)
	}
	return secret
}

// generateSessionID creates a random session ID
func generateSessionID() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// createSession creates a new authenticated session
func createSession(authMethod, paynym, paynymName, paynymAvatar string) (*Session, string) {
	sessionID := generateSessionID()
	// Get session expiry from config (default 24 hours)
	c := getConfig()
	expiryHours := c.Auth.SessionExpiry
	if expiryHours <= 0 {
		expiryHours = 24
	}
	session := &Session{
		LoggedIn:     true,
		AuthMethod:   authMethod,
		Paynym:       paynym,
		PaynymName:   paynymName,
		PaynymAvatar: paynymAvatar,
		CreatedAt:    time.Now(),
		ExpiresAt:    time.Now().Add(time.Duration(expiryHours) * time.Hour),
	}
	authSessionsMu.Lock()
	authSessions[sessionID] = session
	authSessionsMu.Unlock()
	return session, sessionID
}

// getSession retrieves a session by ID
func getSession(sessionID string) *Session {
	authSessionsMu.RLock()
	defer authSessionsMu.RUnlock()
	session, exists := authSessions[sessionID]
	if !exists {
		return nil
	}
	if time.Now().After(session.ExpiresAt) {
		return nil
	}
	return session
}

// deleteSession removes a session
func deleteSession(sessionID string) {
	authSessionsMu.Lock()
	delete(authSessions, sessionID)
	authSessionsMu.Unlock()
}

// generateJWTToken creates a JWT token for a session
func generateJWTToken(sessionID string) (string, error) {
	claims := jwt.MapClaims{
		"session_id": sessionID,
		"exp":        time.Now().Add(24 * time.Hour).Unix(),
		"iat":        time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// verifyJWTToken validates a JWT token and returns the session ID
func verifyJWTToken(tokenString string) (string, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil {
		return "", err
	}
	if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
		if sessionID, ok := claims["session_id"].(string); ok {
			return sessionID, nil
		}
	}
	return "", fmt.Errorf("invalid token")
}

// authMiddleware checks if user is authenticated
func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := getConfig()
		// If auth mode is "none", skip authentication
		if c.Auth.Mode == "" || c.Auth.Mode == "none" {
			next(w, r)
			return
		}

		// Get session cookie
		cookie, err := r.Cookie("noderouter_session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Verify JWT token
		sessionID, err := verifyJWTToken(cookie.Value)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Check session exists and is valid
		session := getSession(sessionID)
		if session == nil || !session.LoggedIn {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		// Security: Set security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "no-referrer")

		next(w, r)
	}
}

// handleLoginPage serves the login page
func handleLoginPage(w http.ResponseWriter, r *http.Request) {
	c := getConfig()
	// If auth is disabled, redirect to dashboard
	if c.Auth.Mode == "" || c.Auth.Mode == "none" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	// Security headers for login page
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-XSS-Protection", "1; mode=block")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := loginTpl.Execute(w, map[string]interface{}{
		"AuthMode": c.Auth.Mode,
	}); err != nil {
		log.Printf("[HTTP] Login template error: %v", err)
		http.Error(w, "Template error", 500)
	}
}

// checkRateLimit returns true if the IP has exceeded the rate limit
func checkRateLimit(ip string, maxRequests int64, windowSeconds int64) bool {
	now := time.Now().Unix()
	authRateLimitMu.Lock()
	defer authRateLimitMu.Unlock()
	
	lastTime, exists := authRateLimit[ip]
	if !exists || now-lastTime > windowSeconds {
		authRateLimit[ip] = now
		return false
	}
	
	// Count requests in window
	count := int64(0)
	for _, t := range authRateLimit {
		if now-t < windowSeconds {
			count++
		}
	}
	
	if count >= maxRequests {
		return true
	}
	
	authRateLimit[ip] = now
	return false
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Fall back to RemoteAddr
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		return r.RemoteAddr
	}
	return ip
}

// handlePasswordLogin processes password-based login
func handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Rate limiting: 5 attempts per 60 seconds per IP
	clientIP := getClientIP(r)
	if checkRateLimit(clientIP, 5, 60) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"success":false,"message":"Too many attempts, please try again later"}`))
		return
	}
	
	c := getConfig()

	if c.Auth.Mode != "password" && c.Auth.Mode != "both" {
		w.Write([]byte(`{"success":false,"message":"Password auth not enabled"}`))
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Write([]byte(`{"success":false,"message":"Invalid JSON"}`))
		return
	}

	if req.Password == "" {
		w.Write([]byte(`{"success":false,"message":"Password required"}`))
		return
	}

	// Verify password against bcrypt hash
	if c.Auth.PasswordHash == "" {
		w.Write([]byte(`{"success":false,"message":"No password configured"}`))
		return
	}

	err := bcrypt.CompareHashAndPassword([]byte(c.Auth.PasswordHash), []byte(req.Password))
	if err != nil {
		w.Write([]byte(`{"success":false,"message":"Invalid password"}`))
		return
	}

	// Create session
	session, sessionID := createSession("password", "", "", "")
	token, err := generateJWTToken(sessionID)
	if err != nil {
		w.Write([]byte(`{"success":false,"message":"Failed to create session"}`))
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "noderouter_session",
		Value:    token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // Allow over http for Tor hidden service
	})

	log.Printf("[Auth] Password login successful from %s", clientIP)
	w.Write([]byte(fmt.Sprintf(`{"success":true,"token":"%s"}`, token)))
}

// handleAuth47URI generates an Auth47 challenge URI
func handleAuth47URI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := getConfig()

	if c.Auth.Mode != "auth47" && c.Auth.Mode != "both" {
		w.Write([]byte(`{"success":false,"message":"Auth47 not enabled"}`))
		return
	}

	if c.Auth.AdminPaymentCode == "" {
		w.Write([]byte(`{"success":false,"message":"No admin payment code configured"}`))
		return
	}

	// Generate nonce
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		w.Write([]byte(`{"success":false,"message":"Failed to generate nonce"}`))
		return
	}
	nonce := hex.EncodeToString(nonceBytes)

	// Calculate expiry (10 minutes from now)
	expiry := time.Now().Add(10 * time.Minute).Unix()

	// Build Auth47 URI - prefer Tor hostname if available
	callbackHost := getTorHostname()
	scheme := "http"
	if callbackHost == "" {
		// Fallback to request host
		if r.TLS != nil {
			scheme = "https"
		}
		callbackHost = r.Host
	}
	// Ensure .onion uses http scheme
	if strings.HasSuffix(callbackHost, ".onion") {
		scheme = "http"
	}
	callbackURL := fmt.Sprintf("%s://%s/auth/auth47/callback", scheme, callbackHost)
	uri := fmt.Sprintf("auth47://%s?c=%s&e=%d&r=%s", nonce, callbackURL, expiry, callbackURL)

	// Store pending auth
	auth47PendingMu.Lock()
	auth47Pending[nonce] = &Auth47Pending{
		Nonce:     nonce,
		CreatedAt: time.Now(),
		Verified:  false,
	}
	auth47PendingMu.Unlock()

	log.Printf("[Auth47] Generated URI for nonce: %s", nonce)
	w.Write([]byte(fmt.Sprintf(`{"success":true,"nonce":"%s","uri":"%s","payment_code":"%s","expires":%d}`,
		nonce, uri, c.Auth.AdminPaymentCode, expiry)))
}

// handleAuth47Callback processes Auth47 wallet callback
func handleAuth47Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := getConfig()

	var proof struct {
		Challenge string `json:"challenge"`
		Nym       string `json:"nym"`
		Signature string `json:"signature"`
	}
	if err := json.NewDecoder(r.Body).Decode(&proof); err != nil {
		w.Write([]byte(`{"success":false,"message":"Invalid JSON"}`))
		return
	}

	if proof.Challenge == "" || proof.Nym == "" || proof.Signature == "" {
		w.Write([]byte(`{"success":false,"message":"Missing required fields"}`))
		return
	}

	// Verify payment code matches configured one
	if proof.Nym != c.Auth.AdminPaymentCode {
		w.Write([]byte(`{"success":false,"message":"Payment code not authorized"}`))
		return
	}

	// Extract nonce from challenge URL
	challengeURL, err := url.Parse(proof.Challenge)
	if err != nil {
		w.Write([]byte(`{"success":false,"message":"Invalid challenge URL"}`))
		return
	}
	nonce := challengeURL.Hostname()
	if nonce == "" {
		// Try path for format like //nonce?...
		nonce = strings.TrimPrefix(challengeURL.Path, "/")
		nonce = strings.Split(nonce, "?")[0]
	}

	// Check nonce exists
	auth47PendingMu.Lock()
	pending, exists := auth47Pending[nonce]
	if !exists {
		auth47PendingMu.Unlock()
		w.Write([]byte(`{"success":false,"message":"Invalid or expired nonce"}`))
		return
	}
	if pending.Verified {
		auth47PendingMu.Unlock()
		w.Write([]byte(`{"success":false,"message":"Nonce already used"}`))
		return
	}
	pending.Verified = true
	pending.Paynym = proof.Nym
	auth47PendingMu.Unlock()

	log.Printf("[Auth47] Verification successful for %s", proof.Nym)

	// Fetch Paynym avatar
	avatarURL := fetchPaynymAvatar(proof.Nym)
	paynymName := proof.Nym

	// Try to get paynym name from paynym.rs
	name, _ := fetchPaynymName(proof.Nym)
	if name != "" {
		paynymName = name
	}

	// Create session
	session, sessionID := createSession("auth47", proof.Nym, paynymName, avatarURL)
	token, err := generateJWTToken(sessionID)
	if err != nil {
		w.Write([]byte(`{"success":false,"message":"Failed to create session"}`))
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "noderouter_session",
		Value:    token,
		Path:     "/",
		Expires:  session.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false, // Allow over http for Tor hidden service
	})

	w.Write([]byte(fmt.Sprintf(`{"success":true,"token":"%s","paynym":"%s","paynym_name":"%s","paynym_avatar":"%s"}`,
		token, proof.Nym, paynymName, avatarURL)))
}

// handleAuth47Status polls auth status for a nonce
func handleAuth47Status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")

	nonce := strings.TrimPrefix(r.URL.Path, "/auth/auth47/status/")
	if nonce == "" {
		w.Write([]byte(`{"status":"invalid"}`))
		return
	}

	auth47PendingMu.RLock()
	pending, exists := auth47Pending[nonce]
	auth47PendingMu.RUnlock()

	if !exists {
		w.Write([]byte(`{"status":"invalid"}`))
		return
	}

	if pending.Verified {
		// Create session and set cookie when status is polled as verified
		session, sessionID := createSession("auth47", pending.Paynym, "", "")
		token, err := generateJWTToken(sessionID)
		if err == nil {
			http.SetCookie(w, &http.Cookie{
				Name:     "noderouter_session",
				Value:    token,
				Path:     "/",
				Expires:  session.ExpiresAt,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
				Secure:   false,
			})
		}
		w.Write([]byte(`{"status":"verified","paynym":"` + pending.Paynym + `"}`))
		// Clean up after a delay
		go func() {
			time.Sleep(5 * time.Second)
			auth47PendingMu.Lock()
			delete(auth47Pending, nonce)
			auth47PendingMu.Unlock()
		}()
		return
	}

	w.Write([]byte(`{"status":"pending"}`))
}

// handleLogout processes logout
func handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	cookie, err := r.Cookie("noderouter_session")
	if err == nil {
		sessionID, err := verifyJWTToken(cookie.Value)
		if err == nil {
			deleteSession(sessionID)
		}
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "noderouter_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	w.Write([]byte(`{"success":true,"message":"Logged out"}`))
}

// fetchPaynymAvatar gets the avatar URL for a payment code
func fetchPaynymAvatar(paymentCode string) string {
	resp, err := http.Post("https://paynym.rs/api/v1/nym/", "application/json",
		strings.NewReader(fmt.Sprintf(`{"nym":"%s"}`, paymentCode)))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var data struct {
		Codes []struct {
			Code string `json:"code"`
		} `json:"codes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return ""
	}
	if len(data.Codes) > 0 && data.Codes[0].Code != "" {
		return fmt.Sprintf("https://paynym.rs/%s/avatar", data.Codes[0].Code)
	}
	return ""
}

// fetchPaynymName gets the paynym name for a payment code
func fetchPaynymName(paymentCode string) (string, error) {
	resp, err := http.Post("https://paynym.rs/api/v1/nym/", "application/json",
		strings.NewReader(fmt.Sprintf(`{"nym":"%s"}`, paymentCode)))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		NymName string `json:"nymName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", err
	}
	return data.NymName, nil
}

// ==================== Tor Hidden Service ====================

// startTorHiddenService reads the hostname from Tor data directory if enabled
func startTorHiddenService() {
	cfg := getConfig()
	if !cfg.Tor.Enabled {
		log.Println("[Tor] Hidden service disabled in config")
		return
	}

	torDataDir := "/var/lib/tor/noderouter"
	hostnameFile := torDataDir + "/hostname"

	// Wait for hostname file to be created (Tor is started by entrypoint.sh)
	go func() {
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			hostname, err := os.ReadFile(hostnameFile)
			if err == nil && len(hostname) > 0 {
				hostnameStr := strings.TrimSpace(string(hostname))
				torHostnameMu.Lock()
				torHostname = hostnameStr
				torHostnameMu.Unlock()
				log.Printf("[Tor] Hidden service ready: %s", hostnameStr)
				return
			}
		}
		log.Println("[Tor] Timeout waiting for hostname")
	}()
}

// getTorHostname returns the current Tor hidden service hostname
func getTorHostname() string {
	torHostnameMu.RLock()
	defer torHostnameMu.RUnlock()
	return torHostname
}

// ==================== Main ====================

var tpl *template.Template
var loginTpl *template.Template

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

	// Initialize JWT secret
	jwtSecret = generateJWTSecret()
	log.Println("[Auth] JWT secret generated")

	// Parse login template
	loginTpl = template.Must(template.New("login.html").ParseFS(embeddedFiles, "templates/login.html"))

	pollAll()

	// Start Tor hidden service if enabled
	startTorHiddenService()

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

	// Notification checker goroutine (separate from dashboard refresh)
	go func() {
		for {
			c := getConfig()
			freq := time.Duration(c.Notifications.CheckFreq) * time.Second
			if freq < 10*time.Second {
				freq = 10 * time.Second
			}
			if freq > 300*time.Second {
				freq = 300 * time.Second
			}
			ticker := time.NewTicker(freq)
			<-ticker.C
			ticker.Stop()

			// Run notification check with current data
			dataMu.RLock()
			mp := dashData.Mempool
			btc := dashData.Bitcoin
			dataMu.RUnlock()
			checkNotifications(mp, btc)
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

	// Onion-Location middleware - set header on all responses when Tor is ready
	onionMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			hostname := getTorHostname()
			if hostname != "" {
				w.Header().Set("Onion-Location", "http://"+hostname+r.URL.Path)
			}
			next(w, r)
		}
	}

	// Auth routes (public)
	http.HandleFunc("/login", onionMiddleware(handleLoginPage))
	http.HandleFunc("/auth/login", onionMiddleware(handlePasswordLogin))
	http.HandleFunc("/auth/auth47/uri", onionMiddleware(handleAuth47URI))
	http.HandleFunc("/auth/auth47/callback", onionMiddleware(handleAuth47Callback))
	http.HandleFunc("/auth/auth47/status/", onionMiddleware(handleAuth47Status))
	http.HandleFunc("/auth/logout", onionMiddleware(handleLogout))

	// Protected routes
	http.HandleFunc("/", onionMiddleware(authMiddleware(func(w http.ResponseWriter, r *http.Request) {
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
	})))

	http.HandleFunc("/sse/stream", onionMiddleware(authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		sseHub.ServeHTTP(w, r)
	})))
	http.HandleFunc("/api/notifications", onionMiddleware(authMiddleware(handleNotificationsAPI)))

	// Paynym avatar proxy (public for login page)
	http.HandleFunc("/api/paynym/avatar", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		code := r.URL.Query().Get("code")
		if code == "" {
			w.Write([]byte(`{"error":"Missing code parameter"}`))
			return
		}
		// Fetch from paynym.rs
		resp, err := http.Post("https://paynym.rs/api/v1/nym/", "application/json",
			strings.NewReader(fmt.Sprintf(`{"nym":"%s"}`, code)))
		if err != nil {
			w.Write([]byte(`{"error":"Failed to fetch paynym"}`))
			return
		}
		defer resp.Body.Close()
		var data struct {
			Codes []struct {
				Code string `json:"code"`
			} `json:"codes"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			w.Write([]byte(`{"error":"Invalid response"}`))
			return
		}
		if len(data.Codes) > 0 && data.Codes[0].Code != "" {
			w.Write([]byte(fmt.Sprintf(`{"avatar_url":"https://paynym.rs/%s/avatar"}`, data.Codes[0].Code)))
		} else {
			w.Write([]byte(`{"avatar_url":""}`))
		}
	})

	// QR code generation endpoint (public for login page)
	http.HandleFunc("/api/qr", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		text := r.URL.Query().Get("text")
		if text == "" {
			w.Write([]byte(`{"error":"Missing text parameter"}`))
			return
		}
		png, err := qrcode.Encode(text, qrcode.Medium, 256)
		if err != nil {
			w.Write([]byte(fmt.Sprintf(`{"error":"%s"}`, err.Error())))
			return
		}
		w.Write([]byte(fmt.Sprintf(`{"qr":"data:image/png;base64,%s"}`, base64.StdEncoding.EncodeToString(png))))
	})

	log.Printf("[Main] Server on 0.0.0.0:%s", *port)
	log.Printf("[Main] Dashboard at http://localhost:%s", *port)
	log.Println("[Main] Ready")

	if err := http.ListenAndServe(":"+*port, nil); err != nil {
		log.Fatalf("[Main] %v", err)
	}
}

// handleNotificationsAPI handles GET and POST requests for notification settings
func handleNotificationsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		c := getConfig()
		notifMu.RLock()
		txWatches := []TxWatchEntry{}
		if notifSettings != nil {
			txWatches = make([]TxWatchEntry, len(notifSettings.TxWatches))
			copy(txWatches, notifSettings.TxWatches)
		}
		notifMu.RUnlock()

		resp := NotifSettingsResponse{
			RefreshInterval:     c.GlobalRefreshInterval,
			ShowLatency:         c.ShowLatency,
			BtcBlocksCount:      c.BitcoinCore.RecentBlocksCount,
			XmrBlocksCount:      c.Monero.RecentBlocksCount,
			ConnBtcRpc:          c.BitcoinCore.RPCAddress,
			ConnBtcUser:         c.BitcoinCore.RPCUser,
			ConnBtcPass:         c.BitcoinCore.RPCPassword,
			SvcMpEnabled:        c.Mempool.Enabled,
			ConnMpApi:           c.Mempool.APIEndpoint,
			SvcFulEnabled:       c.Fulcrum.Enabled,
			ConnFulcrum:         fmt.Sprintf("%s:%d", c.Fulcrum.RPCAddress, c.Fulcrum.RPCPort),
			SvcXmrEnabled:       c.Monero.Enabled,
			ConnXmrRpc:          c.Monero.RPCAddress,
			GotifyURL:           c.Notifications.GotifyURL,
			GotifyToken:         c.Notifications.GotifyToken,
			GotifyConfigured:    c.Notifications.GotifyURL != "" && c.Notifications.GotifyToken != "",
			NotifEnabled:        c.Notifications.Enabled,
			CheckFreq:           c.Notifications.CheckFreq,
			FeeNotifEnabled:     func() bool { if notifSettings != nil { return notifSettings.FeeEnabled }; return false }(),
			FeeThreshold:        func() float64 { if notifSettings != nil { return notifSettings.FeeThreshold }; return 0 }(),
			FeeAboveThreshold:   func() bool { if notifSettings != nil { return notifSettings.FeeAboveThreshold }; return false }(),
			NewBlockNotif:       func() bool { if notifSettings != nil { return notifSettings.NewBlockEnabled }; return false }(),
			SpecificBlockNotif:  func() bool { if notifSettings != nil { return notifSettings.SpecificBlockEnabled }; return false }(),
			SpecificBlockHeight: func() int { if notifSettings != nil { return notifSettings.SpecificBlockHeight }; return 0 }(),
			TxWatches:           txWatches,
			TxTargetConfs:       1,
		}
		json.NewEncoder(w).Encode(resp)

	case http.MethodPost:
		var req struct {
			Action              string  `json:"action"`
			RefreshInterval     int     `json:"refresh_interval"`
			ShowLatency         bool    `json:"show_latency"`
			BtcBlocksCount      int     `json:"btc_blocks_count"`
			XmrBlocksCount      int     `json:"xmr_blocks_count"`
			SvcMpEnabled        bool    `json:"svc_mp_enabled"`
			SvcFulEnabled       bool    `json:"svc_ful_enabled"`
			SvcXmrEnabled       bool    `json:"svc_xmr_enabled"`
			ConnBtcRpc          string  `json:"conn_btc_rpc"`
			ConnBtcUser         string  `json:"conn_btc_user"`
			ConnBtcPass         string  `json:"conn_btc_pass"`
			ConnMpApi           string  `json:"conn_mp_api"`
			ConnFulcrum         string  `json:"conn_fulcrum"`
			ConnXmrRpc          string  `json:"conn_xmr_rpc"`
			GotifyURL           string  `json:"gotify_url"`
			GotifyToken         string  `json:"gotify_token"`
			NotifEnabled        bool    `json:"notif_enabled"`
			CheckFreq           int     `json:"check_freq"`
			FeeNotifEnabled     bool    `json:"fee_notif_enabled"`
			FeeThreshold        float64 `json:"fee_threshold"`
			FeeAboveThreshold   bool    `json:"fee_above_threshold"`
			NewBlockNotif       bool    `json:"new_block_notif"`
			SpecificBlockNotif  bool    `json:"specific_block_notif"`
			SpecificBlockHeight int     `json:"specific_block_height"`
			TxID                string  `json:"txid"`
			TxTargetConfs       int     `json:"tx_target_confs"`
			TestName            string  `json:"test_name"`
			TestURL             string  `json:"test_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Write([]byte(`{"success":false,"message":"invalid json"}`))
			return
		}

		if req.Action == "test" {
			if req.GotifyURL == "" || req.GotifyToken == "" {
				w.Write([]byte(`{"success":false,"message":"Gotify URL and Token required"}`))
				return
			}
			err := sendGotifyTest(req.GotifyURL, req.GotifyToken)
			if err != nil {
				w.Write([]byte(fmt.Sprintf(`{"success":false,"message":"%s"}`, err.Error())))
			} else {
				w.Write([]byte(`{"success":true,"message":"Test notification sent successfully"}`))
			}
			return
		}

		if req.Action == "test_connection" {
			var success bool
			var msg string
			switch req.TestName {
			case "bitcoin":
				rpcURL := strings.TrimRight(req.TestURL, "/")
				result := rpcCall(rpcURL, req.ConnBtcUser, req.ConnBtcPass, "getblockchaininfo", nil)
				if result != nil {
					success = true
					msg = "Bitcoin Core connected"
				} else {
					msg = "Bitcoin Core connection failed"
				}
			case "mempool":
				apiURL := strings.TrimRight(req.TestURL, "/")
				// Auto-append /api if not present
				if !strings.HasSuffix(apiURL, "/api") {
					apiURL = apiURL + "/api"
				}
				var fees map[string]float64
				err := getJSON(apiURL+"/v1/fees/recommended", &fees)
				if err == nil {
					success = true
					msg = "Mempool connected"
				} else {
					msg = "Mempool connection failed"
				}
			case "fulcrum":
				parts := strings.Split(req.TestURL, ":")
				if len(parts) == 2 {
					addr := parts[0]
					var port int
					fmt.Sscanf(parts[1], "%d", &port)
					conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", addr, port))
					if err == nil {
						conn.Close()
						success = true
						msg = "Fulcrum connected"
					} else {
						msg = "Fulcrum connection failed"
					}
				} else {
					msg = "Invalid Fulcrum address"
				}
			case "monero":
				rpcURL := strings.TrimRight(req.TestURL, "/")
				var info map[string]interface{}
				err := getJSON(rpcURL+"/get_info", &info)
				if err == nil {
					success = true
					msg = "Monero connected"
				} else {
					msg = "Monero connection failed"
				}
			default:
				msg = "Unknown service"
			}
			w.Write([]byte(fmt.Sprintf(`{"success":%t,"message":"%s"}`, success, msg)))
			return
		}

		if req.Action == "clear_tx" {
			notifMu.Lock()
			if notifSettings != nil {
				notifSettings.TxWatches = nil
			}
			notifMu.Unlock()
			w.Write([]byte(`{"success":true,"message":"All TX watches cleared"}`))
			return
		}

		if req.Action == "save" {
			cfgMu.Lock()
			c := cfg
			// Global settings
			if req.RefreshInterval >= 5 && req.RefreshInterval <= 120 {
				c.GlobalRefreshInterval = req.RefreshInterval
			}
			c.ShowLatency = req.ShowLatency
			if req.BtcBlocksCount >= 6 && req.BtcBlocksCount <= 30 {
				c.BitcoinCore.RecentBlocksCount = req.BtcBlocksCount
			}
			if req.XmrBlocksCount >= 6 && req.XmrBlocksCount <= 30 {
				c.Monero.RecentBlocksCount = req.XmrBlocksCount
			}
			// Service connections
			c.BitcoinCore.Enabled = true
			if req.ConnBtcRpc != "" {
				c.BitcoinCore.RPCAddress = req.ConnBtcRpc
			}
			if req.ConnBtcUser != "" {
				c.BitcoinCore.RPCUser = req.ConnBtcUser
			}
			if req.ConnBtcPass != "" {
				c.BitcoinCore.RPCPassword = req.ConnBtcPass
			}
			c.Mempool.Enabled = req.SvcMpEnabled
			if req.ConnMpApi != "" {
				c.Mempool.APIEndpoint = req.ConnMpApi
			}
			c.Fulcrum.Enabled = req.SvcFulEnabled
			if req.ConnFulcrum != "" {
				parts := strings.Split(req.ConnFulcrum, ":")
				if len(parts) == 2 {
					c.Fulcrum.RPCAddress = parts[0]
					fmt.Sscanf(parts[1], "%d", &c.Fulcrum.RPCPort)
				}
			}
			c.Monero.Enabled = req.SvcXmrEnabled
			if req.ConnXmrRpc != "" {
				c.Monero.RPCAddress = req.ConnXmrRpc
			}
			// Notifications
			c.Notifications.GotifyURL = req.GotifyURL
			if req.GotifyToken != "" {
				c.Notifications.GotifyToken = req.GotifyToken
			}
			c.Notifications.Enabled = req.NotifEnabled
			if req.CheckFreq >= 10 && req.CheckFreq <= 300 {
				c.Notifications.CheckFreq = req.CheckFreq
			}
			cfgMu.Unlock()

			// Update ephemeral notification settings
			notifMu.Lock()
			if notifSettings == nil {
				notifSettings = &NotifSettings{FeeType: "next_block"}
			}
			notifSettings.FeeEnabled = req.FeeNotifEnabled
			notifSettings.FeeThreshold = req.FeeThreshold
			notifSettings.FeeAboveThreshold = req.FeeAboveThreshold
			notifSettings.FeeNotified = false
			notifSettings.NewBlockEnabled = req.NewBlockNotif
			notifSettings.SpecificBlockEnabled = req.SpecificBlockNotif
			notifSettings.SpecificBlockHeight = req.SpecificBlockHeight
			notifSettings.SpecificBlockNotified = false
			notifMu.Unlock()

			// Add TX if provided
			if req.TxID != "" && txIDRegex.MatchString(req.TxID) {
				notifMu.Lock()
				found := false
				for _, existing := range notifSettings.TxWatches {
					if existing.TxID == req.TxID {
						found = true
						break
					}
				}
				if !found {
					notifSettings.TxWatches = append(notifSettings.TxWatches, TxWatchEntry{
						TxID:        req.TxID,
						TargetConfs: req.TxTargetConfs,
					})
				}
				notifMu.Unlock()
			}

			// Write config to file
			if err := writeConfig(); err != nil {
				w.Write([]byte(fmt.Sprintf(`{"success":false,"message":"Failed to save config: %s"}`, err.Error())))
				return
			}

			w.Write([]byte(`{"success":true,"message":"Settings saved to config.yaml"}`))
			return
		}

		if req.Action == "remove_tx" && req.TxID != "" {
			if !txIDRegex.MatchString(req.TxID) {
				w.Write([]byte(`{"success":false,"message":"Invalid TXID"}`))
				return
			}
			notifMu.Lock()
			if notifSettings != nil {
				var filtered []TxWatchEntry
				for _, tx := range notifSettings.TxWatches {
					if tx.TxID != req.TxID {
						filtered = append(filtered, tx)
					}
				}
				notifSettings.TxWatches = filtered
			}
			notifMu.Unlock()
			w.Write([]byte(`{"success":true,"message":"TX removed"}`))
			return
		}

		w.Write([]byte(`{"success":false,"message":"invalid action"}`))

	default:
		w.Write([]byte(`{"success":false,"message":"method not allowed"}`))
	}
}

// sendGotifyTest sends a test notification to Gotify
func sendGotifyTest(gotifyURL, gotifyToken string) error {
	parsed, err := url.Parse(gotifyURL)
	if err != nil {
		return fmt.Errorf("invalid URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("URL must be http or https")
	}

	baseURL := strings.TrimRight(gotifyURL, "/")
	msg := GotifyMessage{
		Title:    "NodeRouter Test",
		Message:  "This is a test notification from NodeRouter. Your Gotify integration is working correctly!",
		Priority: 5,
	}
	body, _ := json.Marshal(msg)

	req, _ := http.NewRequest("POST", baseURL+"/message", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", gotifyToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gotify returned status %d", resp.StatusCode)
	}
	return nil
}

// checkNotifications checks notification conditions
func checkNotifications(mp *MempoolData, btc *BitcoinData) {
	c := getConfig()
	if !c.Notifications.Enabled || c.Notifications.GotifyURL == "" || c.Notifications.GotifyToken == "" {
		return
	}

	notifMu.Lock()
	defer notifMu.Unlock()

	if notifSettings == nil {
		notifSettings = &NotifSettings{FeeType: "next_block"}
	}

	if mp == nil || !mp.Connected {
		return
	}

	// Fee rate notifications
	if notifSettings.FeeEnabled && notifSettings.FeeThreshold > 0 {
		currentRate := mp.Fastest
		shouldNotify := false
		if notifSettings.FeeAboveThreshold {
			shouldNotify = currentRate > notifSettings.FeeThreshold
		} else {
			shouldNotify = currentRate < notifSettings.FeeThreshold
		}

		if shouldNotify && !notifSettings.FeeNotified {
			condition := "fallen below"
			if notifSettings.FeeAboveThreshold {
				condition = "risen above"
			}
			err := sendGotify("Fee Rate Alert",
				fmt.Sprintf("Bitcoin fee rate has %s your threshold of %.1f sat/vB and is currently at %.1f sat/vB",
					condition, notifSettings.FeeThreshold, currentRate), 5)
			if err == nil {
				notifSettings.FeeNotified = true
			}
		} else if !shouldNotify {
			notifSettings.FeeNotified = false
		}
	}

	// New block notifications
	if notifSettings.NewBlockEnabled && btc != nil && btc.Blocks > 0 {
		if lastBlockHeight > 0 && btc.Blocks > lastBlockHeight {
			sendGotify("New Bitcoin Block Mined",
				fmt.Sprintf("Block #%d has been mined on the Bitcoin network", btc.Blocks), 3)
		}
		lastBlockHeight = btc.Blocks
	}

	// Specific block notifications
	if notifSettings.SpecificBlockEnabled && btc != nil && btc.Blocks > 0 {
		if !notifSettings.SpecificBlockNotified && btc.Blocks >= notifSettings.SpecificBlockHeight {
			sendGotify("Target Block Height Reached",
				fmt.Sprintf("Bitcoin block height %d has been reached!", notifSettings.SpecificBlockHeight), 8)
			notifSettings.SpecificBlockNotified = true
		}
	}

	// TX confirmation notifications
	if len(notifSettings.TxWatches) > 0 && mp.Connected {
		apiEndpoint := strings.TrimRight(c.Mempool.APIEndpoint, "/")
		// Auto-append /api if not present
		if !strings.HasSuffix(apiEndpoint, "/api") {
			apiEndpoint = apiEndpoint + "/api"
		}
		var remaining []TxWatchEntry
		for _, watch := range notifSettings.TxWatches {
			if watch.Notified {
				continue
			}
			if !txIDRegex.MatchString(watch.TxID) {
				continue
			}
			var txInfo struct {
				TxID string `json:"txid"`
				Status struct {
					Confirmed    bool `json:"confirmed"`
					BlockHeight  int  `json:"block_height"`
					Confirmations int `json:"confirmations"`
				} `json:"status"`
			}
			err := getJSON(apiEndpoint+"/tx/"+watch.TxID, &txInfo)
			if err != nil {
				remaining = append(remaining, watch)
				continue
			}
			if txInfo.Status.Confirmed {
				confs := txInfo.Status.Confirmations
				if confs == 0 {
					confs = 1
				}
				target := watch.TargetConfs
				if target <= 0 {
					target = 1
				}
				if confs >= target {
					txPreview := watch.TxID
					if len(txPreview) >= 8 {
						txPreview = txPreview[:4] + "..." + txPreview[len(txPreview)-4:]
					}
					sendGotify("Transaction Confirmed",
						fmt.Sprintf("Your Bitcoin transaction (%s) has been confirmed with %d confirmations in block %d",
							txPreview, confs, txInfo.Status.BlockHeight), 8)
					watch.Notified = true
					watch.Confirmations = confs
				}
				remaining = append(remaining, watch)
			} else {
				remaining = append(remaining, watch)
			}
		}
		notifSettings.TxWatches = remaining
	}
}

// GotifyMessage represents a Gotify notification
type GotifyMessage struct {
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

// sendGotify sends a notification to Gotify
func sendGotify(title, message string, priority int) error {
	c := getConfig()
	if !c.Notifications.Enabled || c.Notifications.GotifyURL == "" || c.Notifications.GotifyToken == "" {
		return fmt.Errorf("gotify not configured")
	}

	parsed, err := url.Parse(c.Notifications.GotifyURL)
	if err != nil {
		return fmt.Errorf("invalid gotify URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("gotify URL must be http or https")
	}

	msg := GotifyMessage{
		Title:    title,
		Message:  message,
		Priority: priority,
	}
	body, _ := json.Marshal(msg)

	baseURL := strings.TrimRight(c.Notifications.GotifyURL, "/")
	req, _ := http.NewRequest("POST", baseURL+"/message", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Gotify-Key", c.Notifications.GotifyToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("gotify returned status %d", resp.StatusCode)
	}
	return nil
}
