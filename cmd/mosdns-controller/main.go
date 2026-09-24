package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// ControllerSettings holds the user-configurable routing logic
type ControllerSettings struct {
	Mode               string   `json:"mode"`                 // "whitelist", "blacklist", "custom"
	CustomDefaultRoute string   `json:"custom_default_route"` // "direct" or "proxy" for unmatched domains in custom mode
	LocalDNS           []string `json:"local_dns"`            // e.g. ["223.5.5.5", "119.29.29.29"]
	RemoteDNS          []string `json:"remote_dns"`           // e.g. ["tcp://1.1.1.1:53", "tcp://8.8.8.8:53", "https://1.1.1.1/dns-query"]
	RemoteSocks5       string   `json:"remote_socks5"`        // SOCKS5 proxy for remote DNS queries (e.g. "127.0.0.1:10808")
	ListenAddr         string   `json:"listen_addr"`          // e.g. "127.0.0.1:5353"
	PFTable            string   `json:"pf_table"`             // e.g. "GFW_Proxy"
	MinTTL             int      `json:"min_ttl"`              // min TTL clamp in seconds
	MaxTTL             int      `json:"max_ttl"`              // max TTL clamp in seconds
	PFSocket           string   `json:"pf_socket"`            // path to pf-aliasd.sock
	BlockEnabled       bool     `json:"block_enabled"`        // enable custom-block list
	Concurrent         int      `json:"concurrent"`           // remote query concurrency
	LogLevel           string   `json:"log_level"`            // "info", "warn", "error", "debug"
}

// MosdnsYAML represents the MosDNS v5 configuration schema
type MosdnsYAML struct {
	Log     MosdnsLog      `yaml:"log"`
	Plugins []MosdnsPlugin `yaml:"plugins"`
}

type MosdnsLog struct {
	Level string `yaml:"level"`
	File  string `yaml:"file,omitempty"`
}

type MosdnsPlugin struct {
	Tag  string                 `yaml:"tag"`
	Type string                 `yaml:"type"`
	Args map[string]interface{} `yaml:"args"`
}

var (
	listenAddr   string
	configFile   string
	settingsFile string
	ruleDir      string
	logFile      string
	aliasLogFile string
	rcService    string
	aliasService string
	mu           sync.Mutex
)

func defaultSettings() ControllerSettings {
	return ControllerSettings{
		Mode:               "whitelist",
		CustomDefaultRoute: "proxy",
		LocalDNS:           []string{"223.5.5.5", "119.29.29.29"},
		RemoteDNS:          []string{"tcp://1.1.1.1:53", "tcp://8.8.8.8:53", "https://1.1.1.1/dns-query"},
		RemoteSocks5:       "127.0.0.1:10808",
		ListenAddr:         "127.0.0.1:5353",
		PFTable:            "GFW_Proxy",
		MinTTL:             60,
		MaxTTL:             86400,
		PFSocket:           "/var/run/pf-aliasd.sock",
		BlockEnabled:       true,
		Concurrent:         2,
		LogLevel:           "info",
	}
}

func main() {
	flag.StringVar(&listenAddr, "listen", ":5380", "HTTP server listen address")
	flag.StringVar(&configFile, "config", "/usr/local/etc/mosdns/config.yaml", "Path to mosdns config.yaml")
	flag.StringVar(&settingsFile, "settings", "/usr/local/etc/mosdns/controller_settings.json", "Path to controller settings JSON")
	flag.StringVar(&ruleDir, "rule-dir", "/usr/local/etc/mosdns/rule", "Path to mosdns rule directory")
	flag.StringVar(&logFile, "log-file", "/var/log/mosdns.log", "Path to mosdns log file")
	flag.StringVar(&aliasLogFile, "alias-log", "/var/log/pf-aliasd.log", "Path to pf-aliasd log file")
	flag.StringVar(&rcService, "rc-service", "mosdns", "FreeBSD rc.d service name for mosdns")
	flag.StringVar(&aliasService, "alias-service", "pf_aliasd", "FreeBSD rc.d service name for pf_aliasd")
	flag.Parse()

	// Ensure directories and initial rule files exist
	initDirectories()

	// Load or initialize settings
	settings := loadSettings()
	if !fileExists(configFile) {
		log.Printf("[mosdns-controller] config.yaml not found, generating initial config from settings...")
		if err := generateMosdnsConfig(&settings); err != nil {
			log.Printf("[WARN] Failed to generate initial config.yaml: %v", err)
		}
	}

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/service", handleService)
	http.HandleFunc("/api/rules", handleRules)
	http.HandleFunc("/api/test-domain", handleTestDomain)
	http.HandleFunc("/api/logs", handleLogs)

	log.Printf("[mosdns-controller] Starting MosDNS Web Manager on %s", listenAddr)
	log.Printf("[mosdns-controller] Target config: %s, rule dir: %s", configFile, ruleDir)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("[mosdns-controller] Failed to start server: %v", err)
	}
}

func initDirectories() {
	if dir := filepath.Dir(configFile); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	if ruleDir != "" {
		_ = os.MkdirAll(ruleDir, 0755)
	}

	// Create placeholder custom rule files if missing
	customFiles := []struct {
		name    string
		header  string
		samples []string
	}{
		{
			name:   "custom-direct.txt",
			header: "# 用户自定义直连白名单 (域名直接走国内 DNS 解析，不入 PF 代理表)\n# 支持格式: domain:example.com 或 full:example.com 或直接写 example.com\n",
			samples: []string{
				"domain:apple.com",
				"domain:icloud.com",
				"domain:microsoft.com",
			},
		},
		{
			name:   "custom-proxy.txt",
			header: "# 用户自定义出海代理名单 (域名走海外防污染 DoH，并自动注入 PF 代理表)\n# 支持格式: domain:example.com 或 full:example.com 或直接写 example.com\n",
			samples: []string{
				"domain:openai.com",
				"domain:chatgpt.com",
				"domain:claude.ai",
				"domain:github.com",
			},
		},
		{
			name:   "custom-block.txt",
			header: "# 用户自定义拦截阻止名单 (域名直接阻断/拒答)\n# 支持格式: domain:ad.example.com 或 full:bad.example.com\n",
			samples: []string{
				"# domain:ads.example.com",
			},
		},
	}

	for _, cf := range customFiles {
		p := filepath.Join(ruleDir, cf.name)
		if !fileExists(p) {
			var b strings.Builder
			b.WriteString(cf.header)
			for _, s := range cf.samples {
				b.WriteString(s + "\n")
			}
			_ = os.WriteFile(p, []byte(b.String()), 0644)
		}
	}
}

func fileExists(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

func isLoopbackAddr(addr string) bool {
	clean := strings.TrimPrefix(addr, "tcp://")
	clean = strings.TrimPrefix(clean, "udp://")
	clean = strings.TrimPrefix(clean, "https://")
	return strings.HasPrefix(clean, "127.0.0.1") ||
		strings.HasPrefix(clean, "localhost") ||
		strings.HasPrefix(clean, "[::1]")
}

func loadSettings() ControllerSettings {
	mu.Lock()
	defer mu.Unlock()

	s := defaultSettings()
	targetPath := settingsFile
	if !fileExists(targetPath) {
		// Check fallback controller.yaml path
		altYaml := filepath.Join(filepath.Dir(settingsFile), "controller.yaml")
		if fileExists(altYaml) {
			targetPath = altYaml
		} else {
			_ = saveSettingsLocked(&s)
			return s
		}
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		log.Printf("[WARN] Failed to read settings file: %v, using defaults", err)
		return s
	}
	if err := yaml.Unmarshal(data, &s); err != nil {
		log.Printf("[WARN] Failed to parse settings (%s): %v, using defaults", targetPath, err)
		return s
	}
	return s
}

func saveSettingsLocked(s *ControllerSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(settingsFile); dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
	return os.WriteFile(settingsFile, data, 0644)
}

func generateMosdnsConfig(s *ControllerSettings) error {
	cfg := MosdnsYAML{
		Log: MosdnsLog{
			Level: s.LogLevel,
		},
		Plugins: make([]MosdnsPlugin, 0),
	}

	// 1. Upstream: forward_remote
	remoteUpstreams := make([]map[string]interface{}, 0, len(s.RemoteDNS))
	for _, addr := range s.RemoteDNS {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			u := map[string]interface{}{"addr": addr}
			if s.RemoteSocks5 != "" && !isLoopbackAddr(addr) {
				u["socks5"] = s.RemoteSocks5
				u["enable_pipeline"] = true
			}
			remoteUpstreams = append(remoteUpstreams, u)
		}
	}
	concurrent := s.Concurrent
	if concurrent <= 0 {
		concurrent = 2
	}
	cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
		Tag:  "forward_remote",
		Type: "forward",
		Args: map[string]interface{}{
			"concurrent": concurrent,
			"upstreams":  remoteUpstreams,
		},
	})

	// 2. Upstream: forward_local
	localUpstreams := make([]map[string]interface{}, 0, len(s.LocalDNS))
	for _, addr := range s.LocalDNS {
		addr = strings.TrimSpace(addr)
		if addr != "" {
			localUpstreams = append(localUpstreams, map[string]interface{}{"addr": addr})
		}
	}
	cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
		Tag:  "forward_local",
		Type: "forward",
		Args: map[string]interface{}{
			"upstreams": localUpstreams,
		},
	})

	// 3. Domain Sets
	directFiles := []string{}
	cnPath := filepath.Join(ruleDir, "cn.txt")
	if fileExists(cnPath) {
		directFiles = append(directFiles, cnPath)
	}
	customDirectPath := filepath.Join(ruleDir, "custom-direct.txt")
	if fileExists(customDirectPath) {
		directFiles = append(directFiles, customDirectPath)
	}
	if len(directFiles) > 0 {
		cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
			Tag:  "direct_domain_set",
			Type: "domain_set",
			Args: map[string]interface{}{
				"files": directFiles,
			},
		})
	}

	proxyFiles := []string{}
	gfwPath := filepath.Join(ruleDir, "gfw.txt")
	if fileExists(gfwPath) {
		proxyFiles = append(proxyFiles, gfwPath)
	}
	customProxyPath := filepath.Join(ruleDir, "custom-proxy.txt")
	if fileExists(customProxyPath) {
		proxyFiles = append(proxyFiles, customProxyPath)
	}
	if len(proxyFiles) > 0 {
		cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
			Tag:  "proxy_domain_set",
			Type: "domain_set",
			Args: map[string]interface{}{
				"files": proxyFiles,
			},
		})
	}

	customBlockPath := filepath.Join(ruleDir, "custom-block.txt")
	hasBlockSet := false
	if s.BlockEnabled && fileExists(customBlockPath) {
		hasBlockSet = true
		cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
			Tag:  "block_domain_set",
			Type: "domain_set",
			Args: map[string]interface{}{
				"files": []string{customBlockPath},
			},
		})
	}

	// 4. pf_alias Plugin
	minTTL := s.MinTTL
	if minTTL <= 0 {
		minTTL = 60
	}
	maxTTL := s.MaxTTL
	if maxTTL <= 0 {
		maxTTL = 86400
	}
	pfTable := s.PFTable
	if pfTable == "" {
		pfTable = "GFW_Proxy"
	}
	pfSocket := s.PFSocket
	if pfSocket == "" {
		pfSocket = "/var/run/pf-aliasd.sock"
	}
	cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
		Tag:  "sync_to_pf",
		Type: "pf_alias",
		Args: map[string]interface{}{
			"socket_path": pfSocket,
			"table":       pfTable,
			"min_ttl":     minTTL,
			"max_ttl":     maxTTL,
			"timeout":     "100ms",
			"sync":        true,
		},
	})

	// Match helper sequences
	if len(directFiles) > 0 {
		cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
			Tag:  "query_is_direct_domain",
			Type: "sequence",
			Args: map[string]interface{}{
				"exec": []interface{}{
					map[string]interface{}{"_matches_domain": "direct_domain_set"},
				},
			},
		})
	}
	if len(proxyFiles) > 0 {
		cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
			Tag:  "query_is_proxy_domain",
			Type: "sequence",
			Args: map[string]interface{}{
				"exec": []interface{}{
					map[string]interface{}{"_matches_domain": "proxy_domain_set"},
				},
			},
		})
	}
	if hasBlockSet {
		cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
			Tag:  "query_is_block_domain",
			Type: "sequence",
			Args: map[string]interface{}{
				"exec": []interface{}{
					map[string]interface{}{"_matches_domain": "block_domain_set"},
				},
			},
		})
	}

	// 5. Main Sequence Pipeline
	mainExec := make([]interface{}, 0)

	// Step 0: Custom Block check
	if hasBlockSet {
		mainExec = append(mainExec, map[string]interface{}{
			"if": []interface{}{"query_is_block_domain"},
			"exec": []interface{}{
				"_drop",
			},
		})
	}

	// Mode branch:
	// Mode A: Whitelist mode (指定直连白名单，其余全走代理)
	// Mode B: Blacklist mode (指定代理黑名单，其余全走直连)
	// Mode C: Custom / Hybrid mode (部分直连，部分代理，其余按配置走直连或代理)
	switch s.Mode {
	case "blacklist":
		if len(proxyFiles) > 0 {
			mainExec = append(mainExec, map[string]interface{}{
				"if": []interface{}{"query_is_proxy_domain"},
				"exec": []interface{}{
					"forward_remote",
					"sync_to_pf",
					"_return",
				},
			})
		}
		// Unmatched: direct
		mainExec = append(mainExec, "forward_local", "_return")

	case "custom":
		if len(directFiles) > 0 {
			mainExec = append(mainExec, map[string]interface{}{
				"if": []interface{}{"query_is_direct_domain"},
				"exec": []interface{}{
					"forward_local",
					"_return",
				},
			})
		}
		if len(proxyFiles) > 0 {
			mainExec = append(mainExec, map[string]interface{}{
				"if": []interface{}{"query_is_proxy_domain"},
				"exec": []interface{}{
					"forward_remote",
					"sync_to_pf",
					"_return",
				},
			})
		}
		// Unmatched: check CustomDefaultRoute
		if s.CustomDefaultRoute == "proxy" {
			mainExec = append(mainExec, "forward_remote", "sync_to_pf", "_return")
		} else {
			mainExec = append(mainExec, "forward_local", "_return")
		}

	case "whitelist":
		fallthrough
	default:
		// Default: Whitelist mode
		if len(directFiles) > 0 {
			mainExec = append(mainExec, map[string]interface{}{
				"if": []interface{}{"query_is_direct_domain"},
				"exec": []interface{}{
					"forward_local",
					"_return",
				},
			})
		}
		// Unmatched: remote + sync_to_pf
		mainExec = append(mainExec, "forward_remote", "sync_to_pf", "_return")
	}

	cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
		Tag:  "main_sequence",
		Type: "sequence",
		Args: map[string]interface{}{
			"exec": mainExec,
		},
	})

	// 6. Listeners (UDP and TCP)
	lAddr := s.ListenAddr
	if lAddr == "" {
		lAddr = "127.0.0.1:5353"
	}
	cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
		Tag:  "udp_server",
		Type: "udp_server",
		Args: map[string]interface{}{
			"entry":  "main_sequence",
			"listen": lAddr,
		},
	})
	cfg.Plugins = append(cfg.Plugins, MosdnsPlugin{
		Tag:  "tcp_server",
		Type: "tcp_server",
		Args: map[string]interface{}{
			"entry":  "main_sequence",
			"listen": lAddr,
		},
	})

	// Serialize to YAML
	out, err := yaml.Marshal(&cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config yaml: %w", err)
	}

	header := fmt.Sprintf("# ==============================================================================\n"+
		"# MosDNS v5 Configuration - Generated by MosDNS Controller\n"+
		"# Mode: %s | Default Route: %s | Generated: %s\n"+
		"# ==============================================================================\n\n",
		s.Mode, s.CustomDefaultRoute, time.Now().Format("2006-01-02 15:04:05"))

	return os.WriteFile(configFile, append([]byte(header), out...), 0644)
}

func getPID(proc string) int {
	out, err := exec.Command("pgrep", "-x", proc).Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			if pid, err := strconv.Atoi(lines[0]); err == nil && pid > 0 {
				return pid
			}
		}
	}
	return 0
}

func countFileLines(p string) int {
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	cnt := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			cnt++
		}
	}
	return cnt
}

// ------------------------------------------------------------------------------
// HTTP API Handlers
// ------------------------------------------------------------------------------

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	s := loadSettings()

	mosdnsPID := getPID("mosdns")
	aliasPID := getPID("pf-aliasd")

	ruleStats := map[string]int{
		"cn":            countFileLines(filepath.Join(ruleDir, "cn.txt")),
		"gfw":           countFileLines(filepath.Join(ruleDir, "gfw.txt")),
		"custom_direct": countFileLines(filepath.Join(ruleDir, "custom-direct.txt")),
		"custom_proxy":  countFileLines(filepath.Join(ruleDir, "custom-proxy.txt")),
		"custom_block":  countFileLines(filepath.Join(ruleDir, "custom-block.txt")),
	}

	resp := map[string]interface{}{
		"mosdns_running": mosdnsPID > 0,
		"mosdns_pid":     mosdnsPID,
		"aliasd_running": aliasPID > 0,
		"aliasd_pid":     aliasPID,
		"settings":       s,
		"rule_stats":     ruleStats,
		"timestamp":      time.Now().Unix(),
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method == http.MethodGet {
		s := loadSettings()
		_ = json.NewEncoder(w).Encode(s)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			ControllerSettings
			Restart bool `json:"restart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		mu.Lock()
		s := req.ControllerSettings
		if err := saveSettingsLocked(&s); err != nil {
			mu.Unlock()
			http.Error(w, fmt.Sprintf(`{"error":"failed to save settings: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		if err := generateMosdnsConfig(&s); err != nil {
			mu.Unlock()
			http.Error(w, fmt.Sprintf(`{"error":"failed to generate config: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		mu.Unlock()

		msg := "配置已保存并重新生成 config.yaml"
		if req.Restart {
			_ = exec.Command("service", rcService, "restart").Run()
			msg += "，已重启 MosDNS 服务"
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": msg,
		})
		return
	}

	http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
}

func handleService(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Target string `json:"target"` // "mosdns", "pf_aliasd", "all"
		Action string `json:"action"` // "start", "stop", "restart"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	var services []string
	switch req.Target {
	case "alias", "pf_aliasd":
		services = []string{aliasService}
	case "all":
		services = []string{aliasService, rcService}
	default:
		services = []string{rcService}
	}

	var outBuf bytes.Buffer
	allSuccess := true
	for _, s := range services {
		cmd := exec.Command("service", s, req.Action)
		out, err := cmd.CombinedOutput()
		if err != nil {
			rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", s)
			cmd = exec.Command(rcPath, req.Action)
			out, err = cmd.CombinedOutput()
		}
		outBuf.WriteString(fmt.Sprintf("[%s %s]\n%s\n", s, req.Action, string(out)))
		if err != nil {
			allSuccess = false
		}
	}

	time.Sleep(300 * time.Millisecond)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": allSuccess,
		"output":  outBuf.String(),
		"mosdns":  getPID("mosdns") > 0,
		"aliasd":  getPID("pf-aliasd") > 0,
	})
}

func handleRules(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	allowedFiles := map[string]bool{
		"custom-direct.txt": true,
		"custom-proxy.txt":  true,
		"custom-block.txt":  true,
		"cn.txt":            false, // read-only
		"gfw.txt":           false, // read-only
	}

	filename := r.URL.Query().Get("name")
	if filename == "" {
		// Return summary of all rule files
		type FileSummary struct {
			Name      string `json:"name"`
			Count     int    `json:"count"`
			ReadOnly  bool   `json:"read_only"`
			ModTime   string `json:"mod_time"`
			SizeBytes int64  `json:"size_bytes"`
		}
		list := make([]FileSummary, 0, len(allowedFiles))
		for name, editable := range allowedFiles {
			p := filepath.Join(ruleDir, name)
			cnt := 0
			modTime := "-"
			var sz int64
			if info, err := os.Stat(p); err == nil {
				sz = info.Size()
				modTime = info.ModTime().Format("2006-01-02 15:04:05")
				cnt = countFileLines(p)
			}
			list = append(list, FileSummary{
				Name:      name,
				Count:     cnt,
				ReadOnly:  !editable,
				ModTime:   modTime,
				SizeBytes: sz,
			})
		}
		_ = json.NewEncoder(w).Encode(list)
		return
	}

	editable, ok := allowedFiles[filename]
	if !ok {
		http.Error(w, `{"error":"invalid rule filename"}`, http.StatusBadRequest)
		return
	}

	p := filepath.Join(ruleDir, filename)

	if r.Method == http.MethodGet {
		content, _ := os.ReadFile(p)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"name":      filename,
			"content":   string(content),
			"count":     countFileLines(p),
			"read_only": !editable,
		})
		return
	}

	if r.Method == http.MethodPost {
		if !editable {
			http.Error(w, `{"error":"file is read-only (managed by update-opnbox-rules.sh)"}`, http.StatusForbidden)
			return
		}
		var req struct {
			Content string `json:"content"`
			Reload  bool   `json:"reload"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}

		// Normalize line endings
		lines := strings.Split(strings.ReplaceAll(req.Content, "\r\n", "\n"), "\n")
		var cleanLines []string
		for _, l := range lines {
			trimmed := strings.TrimSpace(l)
			if trimmed != "" {
				cleanLines = append(cleanLines, trimmed)
			}
		}

		if err := os.WriteFile(p, []byte(strings.Join(cleanLines, "\n")+"\n"), 0644); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to write file: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		if req.Reload {
			// Reload mosdns
			_ = exec.Command("service", rcService, "restart").Run()
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"name":    filename,
			"count":   countFileLines(p),
			"message": fmt.Sprintf("%s 保存成功 (有效条目: %d)", filename, countFileLines(p)),
		})
		return
	}

	http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
}

func matchDomainInFile(p, domain string) (bool, string) {
	f, err := os.Open(p)
	if err != nil {
		return false, ""
	}
	defer f.Close()

	domain = strings.ToLower(strings.TrimSpace(domain))
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	if idx := strings.Index(domain, "/"); idx != -1 {
		domain = domain[:idx]
	}
	if idx := strings.Index(domain, ":"); idx != -1 {
		domain = domain[:idx]
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip tags like full:, domain:, keyword:
		target := line
		isFull := false
		if strings.HasPrefix(target, "full:") {
			target = strings.TrimPrefix(target, "full:")
			isFull = true
		} else if strings.HasPrefix(target, "domain:") {
			target = strings.TrimPrefix(target, "domain:")
		}

		if isFull {
			if domain == target {
				return true, line
			}
		} else {
			if domain == target || strings.HasSuffix(domain, "."+target) {
				return true, line
			}
		}
	}
	return false, ""
}

func handleTestDomain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		http.Error(w, `{"error":"domain query parameter is required"}`, http.StatusBadRequest)
		return
	}

	s := loadSettings()

	// 1. Check custom block
	if s.BlockEnabled {
		if matched, pattern := matchDomainInFile(filepath.Join(ruleDir, "custom-block.txt"), domain); matched {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"domain":          domain,
				"action":          "block",
				"action_label":    "拦截阻止 (Block)",
				"matched_file":    "custom-block.txt",
				"matched_pattern": pattern,
				"upstream":        "无 (直接拒答 / 丢弃)",
				"sync_to_pf":      false,
				"explanation":     "命中自定义阻止黑名单，直接阻断解析。",
			})
			return
		}
	}

	// 2. Check Direct
	isDirect := false
	directMatchFile := ""
	directPattern := ""
	if matched, pat := matchDomainInFile(filepath.Join(ruleDir, "custom-direct.txt"), domain); matched {
		isDirect = true
		directMatchFile = "custom-direct.txt (用户自定义直连)"
		directPattern = pat
	} else if matched, pat := matchDomainInFile(filepath.Join(ruleDir, "cn.txt"), domain); matched {
		isDirect = true
		directMatchFile = "cn.txt (GeoSite CN 权威国内库)"
		directPattern = pat
	}

	// 3. Check Proxy
	isProxy := false
	proxyMatchFile := ""
	proxyPattern := ""
	if matched, pat := matchDomainInFile(filepath.Join(ruleDir, "custom-proxy.txt"), domain); matched {
		isProxy = true
		proxyMatchFile = "custom-proxy.txt (用户自定义出海代理)"
		proxyPattern = pat
	} else if matched, pat := matchDomainInFile(filepath.Join(ruleDir, "gfw.txt"), domain); matched {
		isProxy = true
		proxyMatchFile = "gfw.txt (GeoSite GFW 权威出海库)"
		proxyPattern = pat
	}

	// Apply active mode logic
	var action, label, matchedFile, pattern, upstream, explanation string
	var syncPF bool

	switch s.Mode {
	case "blacklist":
		if isProxy {
			action = "proxy"
			label = "出海代理 (Proxy)"
			matchedFile = proxyMatchFile
			pattern = proxyPattern
			upstream = fmt.Sprintf("海外防污染 DoH (%s)", strings.Join(s.RemoteDNS, ", "))
			syncPF = true
			explanation = fmt.Sprintf("黑名单模式下：命中代理规则集合 [%s]，使用海外 DoH 解析，并同步将返回 IP 写入内核 PF 表 <%s> 经 TUN 转发。", matchedFile, s.PFTable)
		} else {
			action = "direct"
			label = "本地直连 (Direct)"
			matchedFile = "默认直连 (黑名单未命中)"
			pattern = "-"
			upstream = fmt.Sprintf("国内本地 DNS (%s)", strings.Join(s.LocalDNS, ", "))
			syncPF = false
			explanation = "黑名单模式下：未命中任何代理黑名单规则，默认走国内 DNS 直连解析，不进 PF 代理表。"
		}

	case "custom":
		if isDirect {
			action = "direct"
			label = "本地直连 (Direct)"
			matchedFile = directMatchFile
			pattern = directPattern
			upstream = fmt.Sprintf("国内本地 DNS (%s)", strings.Join(s.LocalDNS, ", "))
			syncPF = false
			explanation = fmt.Sprintf("混合模式下：命中直连规则 [%s]，走国内 DNS 解析并直连。", matchedFile)
		} else if isProxy {
			action = "proxy"
			label = "出海代理 (Proxy)"
			matchedFile = proxyMatchFile
			pattern = proxyPattern
			upstream = fmt.Sprintf("海外防污染 DoH (%s)", strings.Join(s.RemoteDNS, ", "))
			syncPF = true
			explanation = fmt.Sprintf("混合模式下：命中代理规则 [%s]，走海外 DoH 解析并同步注入内核 PF 表 <%s>。", matchedFile, s.PFTable)
		} else {
			if s.CustomDefaultRoute == "proxy" {
				action = "proxy"
				label = "出海代理 (Proxy - 默认兜底)"
				matchedFile = "未分类/未知域名"
				pattern = "-"
				upstream = fmt.Sprintf("海外防污染 DoH (%s)", strings.Join(s.RemoteDNS, ", "))
				syncPF = true
				explanation = fmt.Sprintf("混合模式下：域名未匹配任何直连或代理名单，按用户配置的兜底策略【默认走代理】，注入 PF 表 <%s> 经 TUN 转发。", s.PFTable)
			} else {
				action = "direct"
				label = "本地直连 (Direct - 默认兜底)"
				matchedFile = "未分类/未知域名"
				pattern = "-"
				upstream = fmt.Sprintf("国内本地 DNS (%s)", strings.Join(s.LocalDNS, ", "))
				syncPF = false
				explanation = "混合模式下：域名未匹配任何直连或代理名单，按用户配置的兜底策略【默认走直连】，不进 PF 代理表。"
			}
		}

	case "whitelist":
		fallthrough
	default:
		if isDirect {
			action = "direct"
			label = "本地直连 (Direct)"
			matchedFile = directMatchFile
			pattern = directPattern
			upstream = fmt.Sprintf("国内本地 DNS (%s)", strings.Join(s.LocalDNS, ", "))
			syncPF = false
			explanation = fmt.Sprintf("白名单模式下：命中明确国内白名单 [%s]，走国内 DNS 解析直连，不增加代理开销。", matchedFile)
		} else {
			action = "proxy"
			label = "出海代理 (Proxy)"
			matchedFile = "未明确国内/海外白名单"
			pattern = "-"
			if isProxy {
				matchedFile = proxyMatchFile
				pattern = proxyPattern
			}
			upstream = fmt.Sprintf("海外防污染 DoH (%s)", strings.Join(s.RemoteDNS, ", "))
			syncPF = true
			explanation = fmt.Sprintf("白名单模式下：未明确在国内白名单中，按出海防污染处理，走海外 DoH 解析并将 IP 零竞态同步写入内核 PF 表 <%s>。", s.PFTable)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"domain":          domain,
		"mode":            s.Mode,
		"action":          action,
		"action_label":    label,
		"matched_file":    matchedFile,
		"matched_pattern": pattern,
		"upstream":        upstream,
		"sync_to_pf":      syncPF,
		"pf_table":        s.PFTable,
		"explanation":     explanation,
	})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	target := r.URL.Query().Get("target") // "mosdns" or "aliasd"
	p := logFile
	if target == "aliasd" {
		p = aliasLogFile
	}

	linesToRead := 80
	if n := r.URL.Query().Get("lines"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 && parsed <= 500 {
			linesToRead = parsed
		}
	}

	content := readLastLines(p, linesToRead)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"target": target,
		"file":   p,
		"lines":  content,
	})
}

func readLastLines(filepath string, maxLines int) []string {
	file, err := os.Open(filepath)
	if err != nil {
		return []string{fmt.Sprintf("(日志文件不存在或尚未生成: %s)", filepath)}
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > maxLines*2 {
			lines = lines[len(lines)-maxLines:]
		}
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return lines
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, nil)
}

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MosDNS Manager - OPN-Box 粗粒度智能分流平台</title>
    <style>
        :root {
            --bg-base: #0b0f19;
            --bg-surface: #111827;
            --bg-elevated: #1f2937;
            --border: #374151;
            --border-focus: #3b82f6;
            --text-main: #f3f4f6;
            --text-muted: #9ca3af;
            --primary: #3b82f6;
            --primary-hover: #2563eb;
            --success: #10b981;
            --warning: #f59e0b;
            --danger: #ef4444;
            --radius-sm: 6px;
            --radius-md: 10px;
            --radius-lg: 14px;
            --shadow: 0 4px 20px -2px rgba(0, 0, 0, 0.5);
        }
        * { box-sizing: border-box; margin: 0; padding: 0; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            background: var(--bg-base);
            color: var(--text-main);
            min-height: 100vh;
            display: flex;
            flex-direction: column;
        }
        header {
            background: rgba(17, 24, 39, 0.85);
            backdrop-filter: blur(12px);
            border-bottom: 1px solid var(--border);
            padding: 1rem 1.5rem;
            display: flex;
            align-items: center;
            justify-content: space-between;
            position: sticky;
            top: 0;
            z-index: 50;
        }
        .brand {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }
        .brand-icon {
            width: 34px;
            height: 34px;
            background: linear-gradient(135deg, #3b82f6, #8b5cf6);
            border-radius: var(--radius-sm);
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 1.1rem;
            font-weight: bold;
        }
        .brand-title {
            font-size: 1.15rem;
            font-weight: 700;
            letter-spacing: -0.02em;
        }
        .brand-subtitle {
            font-size: 0.75rem;
            color: var(--text-muted);
        }
        .header-actions {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }
        .badge {
            display: inline-flex;
            align-items: center;
            gap: 0.35rem;
            padding: 0.25rem 0.65rem;
            border-radius: 9999px;
            font-size: 0.75rem;
            font-weight: 600;
        }
        .badge-running { background: rgba(16, 185, 129, 0.15); color: #34d399; border: 1px solid rgba(16, 185, 129, 0.3); }
        .badge-stopped { background: rgba(239, 68, 68, 0.15); color: #f87171; border: 1px solid rgba(239, 68, 68, 0.3); }
        .badge-mode { background: rgba(59, 130, 246, 0.15); color: #60a5fa; border: 1px solid rgba(59, 130, 246, 0.3); }
        .btn {
            background: var(--bg-elevated);
            color: var(--text-main);
            border: 1px solid var(--border);
            padding: 0.45rem 0.9rem;
            border-radius: var(--radius-sm);
            cursor: pointer;
            font-size: 0.825rem;
            font-weight: 500;
            transition: all 0.15s ease;
            display: inline-flex;
            align-items: center;
            gap: 0.35rem;
        }
        .btn:hover { background: #2d3748; border-color: #4b5563; }
        .btn-primary { background: var(--primary); border-color: var(--primary); }
        .btn-primary:hover { background: var(--primary-hover); border-color: var(--primary-hover); }
        .btn-danger { background: rgba(239, 68, 68, 0.2); border-color: var(--danger); color: #fca5a5; }
        .btn-danger:hover { background: var(--danger); color: #fff; }

        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 1.5rem;
            flex: 1;
            width: 100%;
        }

        .stats-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
            gap: 1rem;
            margin-bottom: 1.5rem;
        }
        .stat-card {
            background: var(--bg-surface);
            border: 1px solid var(--border);
            border-radius: var(--radius-md);
            padding: 1rem;
            display: flex;
            flex-direction: column;
            gap: 0.35rem;
        }
        .stat-label { font-size: 0.75rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.05em; }
        .stat-value { font-size: 1.4rem; font-weight: 700; }
        .stat-sub { font-size: 0.75rem; color: var(--text-muted); }

        .nav-tabs {
            display: flex;
            gap: 0.5rem;
            border-bottom: 1px solid var(--border);
            margin-bottom: 1.5rem;
        }
        .tab-btn {
            background: transparent;
            border: none;
            color: var(--text-muted);
            padding: 0.75rem 1.25rem;
            font-size: 0.9rem;
            font-weight: 600;
            cursor: pointer;
            border-bottom: 2px solid transparent;
            transition: all 0.15s ease;
        }
        .tab-btn:hover { color: var(--text-main); }
        .tab-btn.active {
            color: var(--primary);
            border-bottom-color: var(--primary);
        }

        .tab-content { display: none; }
        .tab-content.active { display: block; }

        .card {
            background: var(--bg-surface);
            border: 1px solid var(--border);
            border-radius: var(--radius-lg);
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            box-shadow: var(--shadow);
        }
        .card-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            margin-bottom: 1.25rem;
            padding-bottom: 0.75rem;
            border-bottom: 1px solid var(--border);
        }
        .card-title { font-size: 1.05rem; font-weight: 700; display: flex; align-items: center; gap: 0.5rem; }

        .mode-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
            gap: 1rem;
            margin-bottom: 1.5rem;
        }
        .mode-card {
            background: var(--bg-elevated);
            border: 2px solid var(--border);
            border-radius: var(--radius-md);
            padding: 1.25rem;
            cursor: pointer;
            transition: all 0.2s ease;
            position: relative;
        }
        .mode-card:hover { border-color: #4b5563; }
        .mode-card.selected {
            border-color: var(--primary);
            background: rgba(59, 130, 246, 0.08);
        }
        .mode-title { font-size: 1rem; font-weight: 700; margin-bottom: 0.35rem; display: flex; align-items: center; justify-content: space-between; }
        .mode-desc { font-size: 0.8rem; color: var(--text-muted); line-height: 1.45; }

        .form-group {
            margin-bottom: 1.25rem;
        }
        .form-label {
            display: block;
            font-size: 0.825rem;
            font-weight: 600;
            margin-bottom: 0.4rem;
            color: #d1d5db;
        }
        .form-hint {
            font-size: 0.75rem;
            color: var(--text-muted);
            margin-top: 0.35rem;
        }
        .form-control {
            width: 100%;
            background: var(--bg-elevated);
            border: 1px solid var(--border);
            border-radius: var(--radius-sm);
            color: var(--text-main);
            padding: 0.55rem 0.75rem;
            font-size: 0.85rem;
            font-family: inherit;
            transition: border-color 0.15s ease;
        }
        .form-control:focus {
            outline: none;
            border-color: var(--border-focus);
            box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.2);
        }
        textarea.form-control {
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
            font-size: 0.8rem;
            line-height: 1.4;
            min-height: 220px;
            resize: vertical;
        }
        .form-row {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
            gap: 1rem;
        }

        .rule-subtabs {
            display: flex;
            gap: 0.5rem;
            margin-bottom: 1rem;
            flex-wrap: wrap;
        }
        .rule-btn {
            background: var(--bg-elevated);
            border: 1px solid var(--border);
            color: var(--text-muted);
            padding: 0.4rem 0.85rem;
            border-radius: var(--radius-sm);
            font-size: 0.8rem;
            font-weight: 600;
            cursor: pointer;
        }
        .rule-btn.active {
            background: rgba(59, 130, 246, 0.2);
            border-color: var(--primary);
            color: #fff;
        }

        .result-box {
            background: var(--bg-elevated);
            border: 1px solid var(--border);
            border-radius: var(--radius-md);
            padding: 1.25rem;
            margin-top: 1rem;
            display: none;
        }
        .result-row {
            display: flex;
            justify-content: space-between;
            padding: 0.4rem 0;
            border-bottom: 1px solid rgba(255, 255, 255, 0.05);
            font-size: 0.85rem;
        }
        .result-row:last-child { border-bottom: none; }
        .result-label { color: var(--text-muted); }
        .result-val { font-weight: 600; }

        .log-box {
            background: #000;
            color: #10b981;
            font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
            font-size: 0.775rem;
            padding: 1rem;
            border-radius: var(--radius-md);
            height: 380px;
            overflow-y: auto;
            white-space: pre-wrap;
            border: 1px solid #1f2937;
            line-height: 1.45;
        }

        .toast {
            position: fixed;
            bottom: 1.5rem;
            right: 1.5rem;
            padding: 0.75rem 1.25rem;
            border-radius: var(--radius-md);
            background: var(--bg-surface);
            color: #fff;
            box-shadow: 0 10px 25px rgba(0,0,0,0.6);
            border: 1px solid var(--border);
            font-size: 0.85rem;
            z-index: 1000;
            display: none;
            animation: fadeIn 0.2s ease;
        }
        @keyframes fadeIn { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: translateY(0); } }
    </style>
</head>
<body>
    <header>
        <div class="brand">
            <div class="brand-icon">M</div>
            <div>
                <div class="brand-title">MosDNS Manager</div>
                <div class="brand-subtitle">OPN-Box 粗粒度智能分流与零首包同步控制台</div>
            </div>
        </div>
        <div class="header-actions">
            <span id="badgeMosdns" class="badge badge-stopped">MosDNS: 离线</span>
            <span id="badgeAliasd" class="badge badge-stopped">pf-aliasd: 离线</span>
            <span id="badgeMode" class="badge badge-mode">模式: 白名单直连</span>
            <button class="btn btn-primary" onclick="restartServices()">重启服务</button>
        </div>
    </header>

    <div class="container">
        <!-- 概览卡片矩阵 -->
        <div class="stats-grid">
            <div class="stat-card">
                <div class="stat-label">分流运行模式</div>
                <div class="stat-value" id="statMode">白名单</div>
                <div class="stat-sub" id="statModeSub">国内直连，其余代理</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">DNS 本地监听</div>
                <div class="stat-value" id="statListen">127.0.0.1:5353</div>
                <div class="stat-sub">UDP + TCP 双协议绑定</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">内核 PF 代理表</div>
                <div class="stat-value" id="statTable">&lt;GFW_Proxy&gt;</div>
                <div class="stat-sub">微秒级零竞态同步入表</div>
            </div>
            <div class="stat-card">
                <div class="stat-label">规则库条目总计</div>
                <div class="stat-value" id="statRulesTotal">0</div>
                <div class="stat-sub">直连 + 代理 + 拦截自定义</div>
            </div>
        </div>

        <!-- 导航标签 -->
        <div class="nav-tabs">
            <button class="tab-btn active" onclick="switchTab('tab-mode')">🎛️ 分流模式与解析器</button>
            <button class="tab-btn" onclick="switchTab('tab-rules')">📋 规则库与自定义列表</button>
            <button class="tab-btn" onclick="switchTab('tab-tester')">🔍 域名分流实时模拟器</button>
            <button class="tab-btn" onclick="switchTab('tab-logs')">📜 运行与同步日志</button>
        </div>

        <!-- 标签页 1: 模式与核心配置 -->
        <div id="tab-mode" class="tab-content active">
            <div class="card">
                <div class="card-header">
                    <div class="card-title">分流模式选择 (粗粒度分流引擎)</div>
                </div>
                <div class="mode-grid">
                    <div class="mode-card selected" id="cardModeWhitelist" onclick="selectMode('whitelist')">
                        <div class="mode-title">
                            <span>白名单直连模式</span>
                            <span class="badge badge-mode">推荐</span>
                        </div>
                        <div class="mode-desc">明确国内域名与自定义直连库走国内本地 DNS 直连；未明确分类或出海域名走海外防污染 DoH 并推入 PF 代理表。</div>
                    </div>
                    <div class="mode-card" id="cardModeBlacklist" onclick="selectMode('blacklist')">
                        <div class="mode-title">
                            <span>黑名单代理模式</span>
                        </div>
                        <div class="mode-desc">明确 GFW 名单与自定义代理库走海外防污染 DoH 并推入 PF 代理表；其余未明确分类域名全部走国内 DNS 直连。</div>
                    </div>
                    <div class="mode-card" id="cardModeCustom" onclick="selectMode('custom')">
                        <div class="mode-title">
                            <span>自定义 / 混合模式</span>
                        </div>
                        <div class="mode-desc">直连名单直连，代理名单代理；对于未命中的未知流量，可自由指定默认走直连还是默认走代理。</div>
                    </div>
                </div>

                <div class="form-group" id="groupCustomDefault" style="display: none;">
                    <label class="form-label">未匹配域名的默认兜底走向 (Default Route for Unmatched)</label>
                    <select class="form-control" id="customDefaultRoute">
                        <option value="proxy">默认走出海代理 (DoH 解析并推入 PF 表)</option>
                        <option value="direct">默认走国内直连 (国内 DNS 解析直出)</option>
                    </select>
                    <div class="form-hint">当域名既不在直连库也不在代理库时采纳的判定策略。</div>
                </div>

                <div class="form-row">
                    <div class="form-group">
                        <label class="form-label">国内直连上游 DNS (逗号或换行分隔)</label>
                        <input type="text" class="form-control" id="localDNS" value="223.5.5.5, 119.29.29.29">
                        <div class="form-hint">负责国内主流域名高速解析 (UDP/TCP)。</div>
                    </div>
                    <div class="form-group">
                        <label class="form-label">海外出海上游 DNS (支持 tcp://1.1.1.1:53, DoH 等)</label>
                        <input type="text" class="form-control" id="remoteDNS" value="tcp://1.1.1.1:53, tcp://8.8.8.8:53">
                        <div class="form-hint">极速推荐 TCP DNS: tcp://1.1.1.1:53 或 DoH: https://1.1.1.1/dns-query</div>
                    </div>
                    <div class="form-group">
                        <label class="form-label">远端 DNS SOCKS5 代理通道</label>
                        <input type="text" class="form-control" id="remoteSocks5" value="127.0.0.1:10808">
                        <div class="form-hint">指向 Xray 本地 SOCKS5 入站 (127.0.0.1:10808)，远端查询全部走代理加密隧道，留空表示直连。</div>
                    </div>
                </div>

                <div class="form-row">
                    <div class="form-group">
                        <label class="form-label">MosDNS 本地监听端口</label>
                        <input type="text" class="form-control" id="listenAddr" value="127.0.0.1:5353">
                    </div>
                    <div class="form-group">
                        <label class="form-label">FreeBSD PF 外部别名表名 (External Alias)</label>
                        <input type="text" class="form-control" id="pfTable" value="GFW_Proxy">
                    </div>
                    <div class="form-group">
                        <label class="form-label">最小 TTL 钳位 (秒)</label>
                        <input type="number" class="form-control" id="minTTL" value="60">
                    </div>
                </div>

                <div style="display: flex; justify-content: flex-end; gap: 0.75rem; margin-top: 1rem;">
                    <button class="btn btn-primary" onclick="saveSettings(true)">💾 保存配置并应用生效</button>
                </div>
            </div>
        </div>

        <!-- 标签页 2: 规则库管理 -->
        <div id="tab-rules" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <div class="card-title">规则库管理与自定义列表编辑</div>
                    <div style="display: flex; gap: 0.5rem;">
                        <button class="btn" onclick="loadRulesSummary()">🔄 刷新统计</button>
                        <button class="btn btn-primary" onclick="saveCurrentRuleFile()">💾 保存当前列表</button>
                    </div>
                </div>

                <div class="rule-subtabs">
                    <button class="rule-btn active" onclick="switchRuleFile('custom-direct.txt')">🟢 自定义直连列表 (custom-direct.txt)</button>
                    <button class="rule-btn" onclick="switchRuleFile('custom-proxy.txt')">🟠 自定义代理列表 (custom-proxy.txt)</button>
                    <button class="rule-btn" onclick="switchRuleFile('custom-block.txt')">🔴 自定义阻止拦截 (custom-block.txt)</button>
                    <button class="rule-btn" onclick="switchRuleFile('cn.txt')">📖 权威国内库 cn.txt (只读)</button>
                    <button class="rule-btn" onclick="switchRuleFile('gfw.txt')">📖 权威出海库 gfw.txt (只读)</button>
                </div>

                <div style="margin-bottom: 0.75rem; display: flex; justify-content: space-between; align-items: center;">
                    <span id="currentRuleInfo" style="font-size: 0.8rem; color: var(--text-muted);">正在编辑: custom-direct.txt</span>
                    <span id="currentRuleCount" class="badge badge-mode">条目数: 0</span>
                </div>

                <textarea id="ruleEditor" class="form-control" placeholder="正在读取规则列表..."></textarea>
            </div>
        </div>

        <!-- 标签页 3: 域名分流实时模拟测试器 -->
        <div id="tab-tester" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <div class="card-title">域名分流规则实时模拟器 (Live Diagnostic)</div>
                </div>
                <p style="font-size: 0.85rem; color: var(--text-muted); margin-bottom: 1rem;">
                    输入任意域名，系统将根据当前生效的分流模式（白名单/黑名单/混合）及内置规则库，实时模拟 MosDNS 内部的处理流水线并输出判定结果：
                </p>
                <div style="display: flex; gap: 0.75rem;">
                    <input type="text" class="form-control" id="testDomainInput" placeholder="输入要测试的域名，例如: bilibili.com, openai.com, netflix.com" onkeydown="if(event.key==='Enter') testDomain()">
                    <button class="btn btn-primary" style="white-space: nowrap;" onclick="testDomain()">⚡ 立即测试分流</button>
                </div>

                <div id="testResultBox" class="result-box">
                    <div class="result-row">
                        <span class="result-label">测试目标域名</span>
                        <span class="result-val" id="resDomain">-</span>
                    </div>
                    <div class="result-row">
                        <span class="result-label">最终分流决策</span>
                        <span class="result-val" id="resAction">-</span>
                    </div>
                    <div class="result-row">
                        <span class="result-label">命中规则文件</span>
                        <span class="result-val" id="resFile">-</span>
                    </div>
                    <div class="result-row">
                        <span class="result-label">上游解析节点</span>
                        <span class="result-val" id="resUpstream">-</span>
                    </div>
                    <div class="result-row">
                        <span class="result-label">内核 PF 代理表注入</span>
                        <span class="result-val" id="resPF">-</span>
                    </div>
                    <div class="result-row" style="flex-direction: column; gap: 0.4rem; padding-top: 0.75rem;">
                        <span class="result-label">判定原理说明</span>
                        <div id="resExplanation" style="font-size: 0.825rem; color: #d1d5db; line-height: 1.5; background: rgba(0,0,0,0.3); padding: 0.65rem; border-radius: var(--radius-sm);">-</div>
                    </div>
                </div>
            </div>
        </div>

        <!-- 标签页 4: 运行与日志 -->
        <div id="tab-logs" class="tab-content">
            <div class="card">
                <div class="card-header">
                    <div class="card-title">系统实时日志</div>
                    <div style="display: flex; gap: 0.5rem;">
                        <select class="form-control" id="logTarget" style="width: auto;" onchange="loadLogs()">
                            <option value="mosdns">MosDNS 运行日志 (/var/log/mosdns.log)</option>
                            <option value="aliasd">pf-aliasd 守护日志 (/var/log/pf-aliasd.log)</option>
                        </select>
                        <button class="btn" onclick="loadLogs()">🔄 刷新日志</button>
                    </div>
                </div>
                <div class="log-box" id="logViewer">正在加载日志...</div>
            </div>
        </div>
    </div>

    <div id="toast" class="toast"></div>

    <script>
        let currentSettings = {};
        let currentRuleName = 'custom-direct.txt';

        function showToast(msg, duration = 3000) {
            const t = document.getElementById('toast');
            t.innerText = msg;
            t.style.display = 'block';
            setTimeout(() => { t.style.display = 'none'; }, duration);
        }

        function switchTab(tabId) {
            document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            event.target.classList.add('active');
            document.getElementById(tabId).classList.add('active');

            if (tabId === 'tab-rules') {
                loadRuleFile(currentRuleName);
            } else if (tabId === 'tab-logs') {
                loadLogs();
            }
        }

        function selectMode(mode) {
            ['whitelist', 'blacklist', 'custom'].forEach(m => {
                const el = document.getElementById('cardMode' + m.charAt(0).toUpperCase() + m.slice(1));
                if (el) el.classList.remove('selected');
            });
            const selEl = document.getElementById('cardMode' + mode.charAt(0).toUpperCase() + mode.slice(1));
            if (selEl) selEl.classList.add('selected');

            document.getElementById('groupCustomDefault').style.display = (mode === 'custom') ? 'block' : 'none';
            currentSettings.mode = mode;
        }

        async function fetchStatus() {
            try {
                const res = await fetch('/api/status');
                const data = await res.json();

                // Status badges
                const bM = document.getElementById('badgeMosdns');
                if (data.mosdns_running) {
                    bM.className = 'badge badge-running';
                    bM.innerText = 'MosDNS: 运行中 (PID ' + data.mosdns_pid + ')';
                } else {
                    bM.className = 'badge badge-stopped';
                    bM.innerText = 'MosDNS: 停止';
                }

                const bA = document.getElementById('badgeAliasd');
                if (data.aliasd_running) {
                    bA.className = 'badge badge-running';
                    bA.innerText = 'pf-aliasd: 运行中 (PID ' + data.aliasd_pid + ')';
                } else {
                    bA.className = 'badge badge-stopped';
                    bA.innerText = 'pf-aliasd: 停止';
                }

                // Mode badge & stats
                const modeMap = {
                    'whitelist': '白名单直连',
                    'blacklist': '黑名单代理',
                    'custom': '混合自定义'
                };
                const modeName = modeMap[data.settings.mode] || data.settings.mode;
                document.getElementById('badgeMode').innerText = '模式: ' + modeName;
                document.getElementById('statMode').innerText = modeName;
                document.getElementById('statListen').innerText = data.settings.listen_addr;
                document.getElementById('statTable').innerText = '<' + data.settings.pf_table + '>';

                let totalRules = 0;
                if (data.rule_stats) {
                    for (const k in data.rule_stats) { totalRules += data.rule_stats[k]; }
                }
                document.getElementById('statRulesTotal').innerText = totalRules.toLocaleString();

                if (!currentSettings.mode) {
                    currentSettings = data.settings;
                    applySettingsToUI(data.settings);
                }
            } catch (err) {
                console.error(err);
            }
        }

        function applySettingsToUI(s) {
            selectMode(s.mode || 'whitelist');
            if (s.custom_default_route) {
                document.getElementById('customDefaultRoute').value = s.custom_default_route;
            }
            if (s.local_dns) {
                document.getElementById('localDNS').value = s.local_dns.join(', ');
            }
            if (s.remote_dns) {
                document.getElementById('remoteDNS').value = s.remote_dns.join(', ');
            }
            if (s.remote_socks5 !== undefined) {
                document.getElementById('remoteSocks5').value = s.remote_socks5;
            }
            if (s.listen_addr) {
                document.getElementById('listenAddr').value = s.listen_addr;
            }
            if (s.pf_table) {
                document.getElementById('pfTable').value = s.pf_table;
            }
            if (s.min_ttl) {
                document.getElementById('minTTL').value = s.min_ttl;
            }
        }

        async function saveSettings(restart) {
            const parseList = (str) => str.split(',').map(s => s.trim()).filter(s => s.length > 0);
            const payload = {
                mode: currentSettings.mode || 'whitelist',
                custom_default_route: document.getElementById('customDefaultRoute').value,
                local_dns: parseList(document.getElementById('localDNS').value),
                remote_dns: parseList(document.getElementById('remoteDNS').value),
                remote_socks5: document.getElementById('remoteSocks5').value.trim(),
                listen_addr: document.getElementById('listenAddr').value.trim(),
                pf_table: document.getElementById('pfTable').value.trim(),
                min_ttl: parseInt(document.getElementById('minTTL').value) || 60,
                max_ttl: 86400,
                pf_socket: '/var/run/pf-aliasd.sock',
                block_enabled: true,
                concurrent: 2,
                log_level: 'info',
                restart: restart
            };

            try {
                const res = await fetch('/api/config', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(payload)
                });
                const data = await res.json();
                if (data.success) {
                    showToast(data.message);
                    fetchStatus();
                } else {
                    showToast('保存失败: ' + (data.error || '未知错误'));
                }
            } catch (err) {
                showToast('网络请求异常: ' + err);
            }
        }

        async function restartServices() {
            showToast('正在重启 MosDNS 与 pf-aliasd 服务...');
            try {
                const res = await fetch('/api/service', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ target: 'all', action: 'restart' })
                });
                const data = await res.json();
                showToast(data.success ? '服务重启完毕！' : '重启出现异常');
                fetchStatus();
            } catch (err) {
                showToast('重启请求失败: ' + err);
            }
        }

        async function switchRuleFile(filename) {
            currentRuleName = filename;
            document.querySelectorAll('.rule-btn').forEach(b => b.classList.remove('active'));
            event.target.classList.add('active');
            loadRuleFile(filename);
        }

        async function loadRuleFile(filename) {
            document.getElementById('currentRuleInfo').innerText = '正在读取: ' + filename;
            const editor = document.getElementById('ruleEditor');
            editor.value = '加载中...';
            try {
                const res = await fetch('/api/rules?name=' + encodeURIComponent(filename));
                const data = await res.json();
                editor.value = data.content || '';
                editor.readOnly = !!data.read_only;
                document.getElementById('currentRuleInfo').innerText = (data.read_only ? '📖 只读库: ' : '✏️ 正在编辑: ') + filename;
                document.getElementById('currentRuleCount').innerText = '有效条目: ' + (data.count || 0);
            } catch (err) {
                editor.value = '读取失败: ' + err;
            }
        }

        async function saveCurrentRuleFile() {
            const editor = document.getElementById('ruleEditor');
            if (editor.readOnly) {
                showToast('该规则文件为系统权威大库，属于只读状态！');
                return;
            }
            try {
                const res = await fetch('/api/rules?name=' + encodeURIComponent(currentRuleName), {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ content: editor.value, reload: true })
                });
                const data = await res.json();
                if (data.success) {
                    showToast(data.message);
                    document.getElementById('currentRuleCount').innerText = '有效条目: ' + data.count;
                    fetchStatus();
                } else {
                    showToast('保存失败: ' + (data.error || '未知'));
                }
            } catch (err) {
                showToast('保存异常: ' + err);
            }
        }

        async function testDomain() {
            const input = document.getElementById('testDomainInput');
            const d = input.value.trim();
            if (!d) {
                showToast('请输入要测试的域名！');
                return;
            }
            try {
                const res = await fetch('/api/test-domain?domain=' + encodeURIComponent(d));
                const data = await res.json();
                document.getElementById('testResultBox').style.display = 'block';
                document.getElementById('resDomain').innerText = data.domain;
                document.getElementById('resAction').innerText = data.action_label;
                document.getElementById('resAction').style.color = (data.action === 'direct') ? '#34d399' : (data.action === 'block' ? '#f87171' : '#60a5fa');
                document.getElementById('resFile').innerText = data.matched_file + (data.matched_pattern && data.matched_pattern !== '-' ? ' (命中: ' + data.matched_pattern + ')' : '');
                document.getElementById('resUpstream').innerText = data.upstream;
                document.getElementById('resPF').innerText = data.sync_to_pf ? '是 (写入 <' + data.pf_table + '> 表)' : '否 (不入代理表)';
                document.getElementById('resExplanation').innerText = data.explanation;
            } catch (err) {
                showToast('测试失败: ' + err);
            }
        }

        async function loadLogs() {
            const target = document.getElementById('logTarget').value;
            const viewer = document.getElementById('logViewer');
            try {
                const res = await fetch('/api/logs?target=' + target);
                const data = await res.json();
                viewer.innerText = (data.lines || []).join('\n');
                viewer.scrollTop = viewer.scrollHeight;
            } catch (err) {
                viewer.innerText = '日志读取失败: ' + err;
            }
        }

        // Init
        fetchStatus();
        setInterval(fetchStatus, 3500);
    </script>
</body>
</html>
`
