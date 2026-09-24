package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	listenAddr string
	configFile string
	dataFile   string
	logFile    string
	rcService  string
	xrayBin    string
	mu         sync.Mutex
)

// AppData persists controller state (nodes, inbounds, rules, subscriptions)
type AppData struct {
	ActiveNodeTag  string          `json:"active_node_tag"`
	Subscriptions  []Subscription  `json:"subscriptions"`
	Nodes          []NodeConfig    `json:"nodes"`
	Inbounds       []CustomInbound `json:"inbounds"`
	Rules          []RoutingRule   `json:"rules"`
	DomainStrategy string          `json:"domain_strategy"`
}

type Subscription struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	LastUpdate int64  `json:"last_update"`
	NodeCount  int    `json:"node_count"`
}

type NodeConfig struct {
	ID          string                 `json:"id"`
	Tag         string                 `json:"tag"`
	Name        string                 `json:"name"`
	Protocol    string                 `json:"protocol"` // vless, vmess, trojan, shadowsocks
	Address     string                 `json:"address"`
	Port        int                    `json:"port"`
	Network     string                 `json:"network"`  // tcp, ws, xhttp, grpc
	Security    string                 `json:"security"` // none, tls, reality
	SNI         string                 `json:"sni,omitempty"`
	Path        string                 `json:"path,omitempty"`
	XHTTPMode   string                 `json:"xhttp_mode,omitempty"`
	SubID       string                 `json:"sub_id,omitempty"`
	PingRTT     int64                  `json:"ping_rtt,omitempty"`
	RawOutbound map[string]interface{} `json:"raw_outbound"`
}

type CustomInbound struct {
	ID       string `json:"id"`
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"` // socks, http
	Listen   string `json:"listen"`   // 0.0.0.0 or 127.0.0.1 or LAN IP
	Port     int    `json:"port"`
	Enabled  bool   `json:"enabled"`
	Sniffing bool   `json:"sniffing"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Comment  string `json:"comment,omitempty"`
}

type RoutingRule struct {
	ID          string   `json:"id"`
	Type        string   `json:"type"` // domain, ip
	Values      []string `json:"values"`
	OutboundTag string   `json:"outbound_tag"` // node tag or direct or block
	Enabled     bool     `json:"enabled"`
	Comment     string   `json:"comment,omitempty"`
}

func main() {
	flag.StringVar(&listenAddr, "listen", ":5384", "HTTP WebUI listen address")
	flag.StringVar(&configFile, "config", "/usr/local/etc/xray/config.json", "Path to xray config.json")
	flag.StringVar(&dataFile, "data", "/usr/local/etc/xray/controller_data.json", "Path to controller state data")
	flag.StringVar(&logFile, "log-file", "/var/log/xray.log", "Path to xray log file")
	flag.StringVar(&rcService, "rc-service", "xray", "FreeBSD rc.d service name")
	flag.StringVar(&xrayBin, "xray-bin", "/usr/local/bin/xray", "Path to xray binary")
	flag.Parse()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/data", handleGetData)
	http.HandleFunc("/api/service", handleService)
	http.HandleFunc("/api/logs", handleLogs)
	http.HandleFunc("/api/subscription/add", handleAddSubscription)
	http.HandleFunc("/api/subscription/update", handleUpdateSubscription)
	http.HandleFunc("/api/subscription/delete", handleDeleteSubscription)
	http.HandleFunc("/api/nodes/import", handleImportNodes)
	http.HandleFunc("/api/nodes/delete", handleDeleteNode)
	http.HandleFunc("/api/nodes/active", handleSetActiveNode)
	http.HandleFunc("/api/nodes/ping", handlePingNodes)
	http.HandleFunc("/api/inbounds/save", handleSaveInbounds)
	http.HandleFunc("/api/routing/save", handleSaveRouting)
	http.HandleFunc("/api/apply", handleApplyConfig)
	http.HandleFunc("/api/config/preview", handlePreviewConfig)

	log.Printf("[xray-controller] Starting Xray Manager on %s", listenAddr)
	log.Printf("[xray-controller] Target config: %s, data: %s", configFile, dataFile)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("[xray-controller] Failed to start server: %v", err)
	}
}

func getPID() int {
	out, err := exec.Command("pgrep", "-x", "xray").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			if pid, err := strconv.Atoi(lines[0]); err == nil && pid > 0 {
				return pid
			}
		}
	}
	pidBytes, err := os.ReadFile("/var/run/xray.pid")
	if err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes))); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

func loadData() (*AppData, error) {
	data, err := os.ReadFile(dataFile)
	if err != nil {
		// Provide default initialized app state
		return &AppData{
			ActiveNodeTag:  "",
			DomainStrategy: "AsIs",
			Subscriptions:  []Subscription{},
			Nodes:          []NodeConfig{},
			Inbounds: []CustomInbound{
				{
					ID:       "in-lan-mixed",
					Tag:      "lan-mixed",
					Protocol: "socks",
					Listen:   "0.0.0.0",
					Port:     10809,
					Enabled:  false,
					Sniffing: true,
					Comment:  "局域网终端专用代理 (SOCKS5)",
				},
				{
					ID:       "in-lan-http",
					Tag:      "lan-http",
					Protocol: "http",
					Listen:   "0.0.0.0",
					Port:     10810,
					Enabled:  false,
					Sniffing: true,
					Comment:  "局域网终端专用代理 (HTTP)",
				},
			},
			Rules: []RoutingRule{
				{
					ID:          "rule-ai",
					Type:        "domain",
					Values:      []string{"geosite:openai", "geosite:anthropic", "domain:claude.ai"},
					OutboundTag: "proxy-default",
					Enabled:     true,
					Comment:     "AI 专用分流 (OpenAI / Claude)",
				},
				{
					ID:          "rule-media",
					Type:        "domain",
					Values:      []string{"geosite:netflix", "geosite:disney"},
					OutboundTag: "proxy-default",
					Enabled:     true,
					Comment:     "海外流媒体",
				},
				{
					ID:          "rule-direct",
					Type:        "domain",
					Values:      []string{"geosite:cn"},
					OutboundTag: "direct",
					Enabled:     true,
					Comment:     "国内域名直连 (可选)",
				},
			},
		}, nil
	}
	var appData AppData
	if err := json.Unmarshal(data, &appData); err != nil {
		return nil, err
	}
	return &appData, nil
}

func saveData(data *AppData) error {
	dir := strings.TrimSuffix(dataFile, "/controller_data.json")
	_ = os.MkdirAll(dir, 0755)
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dataFile, b, 0644)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	pid := getPID()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"running":   pid > 0,
		"pid":       pid,
		"timestamp": time.Now().Unix(),
		"config":    configFile,
	})
}

func handleGetData(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	data, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(data)
}

func handleService(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Action string `json:"action"` // start, stop, restart, test
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if req.Action == "test" {
		cmd := exec.Command(xrayBin, "run", "-test", "-c", configFile)
		out, err := cmd.CombinedOutput()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": err == nil,
			"output":  string(out),
		})
		return
	}

	cmd := exec.Command("service", rcService, req.Action)
	out, err := cmd.CombinedOutput()
	if err != nil {
		rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", rcService)
		cmd = exec.Command(rcPath, req.Action)
		out, err = cmd.CombinedOutput()
	}

	time.Sleep(400 * time.Millisecond)
	pid := getPID()
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": err == nil,
		"output":  string(out),
		"running": pid > 0,
		"pid":     pid,
	})
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	linesToRead := 80
	if n := r.URL.Query().Get("lines"); n != "" {
		if parsed, err := strconv.Atoi(n); err == nil && parsed > 0 && parsed <= 500 {
			linesToRead = parsed
		}
	}

	file, err := os.Open(logFile)
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []string{fmt.Sprintf("暂无日志或日志文件不存在: %s", logFile)},
		})
		return
	}
	defer file.Close()

	data, _ := io.ReadAll(file)
	allLines := strings.Split(string(data), "\n")
	start := 0
	if len(allLines) > linesToRead {
		start = len(allLines) - linesToRead
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": allLines[start:],
	})
}

// Node Link Parsers
func parseNodeURI(raw string, subID string) (*NodeConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty link")
	}

	if strings.HasPrefix(raw, "vless://") {
		return parseVLESS(raw, subID)
	} else if strings.HasPrefix(raw, "vmess://") {
		return parseVMess(raw, subID)
	} else if strings.HasPrefix(raw, "trojan://") {
		return parseTrojan(raw, subID)
	} else if strings.HasPrefix(raw, "ss://") {
		return parseShadowsocks(raw, subID)
	}
	return nil, fmt.Errorf("unsupported protocol scheme")
}

func parseVLESS(link string, subID string) (*NodeConfig, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	uuid := u.User.Username()
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(portStr)

	q := u.Query()
	network := q.Get("type")
	if network == "" {
		network = q.Get("net")
	}
	if network == "" {
		network = "tcp"
	}
	security := q.Get("security")
	if security == "" {
		security = "none"
	}
	sni := q.Get("sni")
	if sni == "" {
		sni = q.Get("serverName")
	}
	path := q.Get("path")
	xhttpMode := q.Get("mode")
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("VLESS-%s:%d", host, port)
	} else {
		name, _ = url.PathUnescape(name)
	}

	tag := fmt.Sprintf("node-%d", time.Now().UnixNano()%1000000)

	// Build raw xray outbound structure
	outbound := map[string]interface{}{
		"tag":      tag,
		"protocol": "vless",
		"settings": map[string]interface{}{
			"vnext": []interface{}{
				map[string]interface{}{
					"address": host,
					"port":    port,
					"users": []interface{}{
						map[string]interface{}{
							"id":         uuid,
							"encryption": "none",
						},
					},
				},
			},
		},
	}

	streamSettings := map[string]interface{}{
		"network":  network,
		"security": security,
	}

	if security == "tls" {
		streamSettings["tlsSettings"] = map[string]interface{}{
			"serverName": sni,
		}
	} else if security == "reality" {
		streamSettings["realitySettings"] = map[string]interface{}{
			"serverName":  sni,
			"fingerprint": q.Get("fp"),
			"publicKey":   q.Get("pbk"),
			"shortId":     q.Get("sid"),
		}
	}

	if network == "xhttp" {
		xhttpSettings := map[string]interface{}{}
		if path != "" {
			xhttpSettings["path"] = path
		}
		if xhttpMode != "" {
			xhttpSettings["mode"] = xhttpMode
		}
		streamSettings["xhttpSettings"] = xhttpSettings
	} else if network == "ws" {
		wsSettings := map[string]interface{}{}
		if path != "" {
			wsSettings["path"] = path
		}
		if q.Get("host") != "" {
			wsSettings["headers"] = map[string]interface{}{"Host": q.Get("host")}
		}
		streamSettings["wsSettings"] = wsSettings
	} else if network == "grpc" {
		streamSettings["grpcSettings"] = map[string]interface{}{
			"serviceName": q.Get("serviceName"),
		}
	}

	outbound["streamSettings"] = streamSettings

	return &NodeConfig{
		ID:          tag,
		Tag:         tag,
		Name:        name,
		Protocol:    "vless",
		Address:     host,
		Port:        port,
		Network:     network,
		Security:    security,
		SNI:         sni,
		Path:        path,
		XHTTPMode:   xhttpMode,
		SubID:       subID,
		RawOutbound: outbound,
	}, nil
}

func parseVMess(link string, subID string) (*NodeConfig, error) {
	b64 := strings.TrimPrefix(link, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		decoded, err = base64.RawURLEncoding.DecodeString(b64)
		if err != nil {
			return nil, err
		}
	}

	var v map[string]interface{}
	if err := json.Unmarshal(decoded, &v); err != nil {
		return nil, err
	}

	name, _ := v["ps"].(string)
	host, _ := v["add"].(string)
	portVal := v["port"]
	var port int
	switch val := portVal.(type) {
	case float64:
		port = int(val)
	case string:
		port, _ = strconv.Atoi(val)
	}
	id, _ := v["id"].(string)
	netType, _ := v["net"].(string)
	tlsType, _ := v["tls"].(string)
	sni, _ := v["sni"].(string)
	path, _ := v["path"].(string)

	tag := fmt.Sprintf("node-%d", time.Now().UnixNano()%1000000)
	security := "none"
	if tlsType == "tls" {
		security = "tls"
	}

	outbound := map[string]interface{}{
		"tag":      tag,
		"protocol": "vmess",
		"settings": map[string]interface{}{
			"vnext": []interface{}{
				map[string]interface{}{
					"address": host,
					"port":    port,
					"users": []interface{}{
						map[string]interface{}{
							"id":       id,
							"alterId":  0,
							"security": "auto",
						},
					},
				},
			},
		},
		"streamSettings": map[string]interface{}{
			"network":  netType,
			"security": security,
		},
	}

	return &NodeConfig{
		ID:          tag,
		Tag:         tag,
		Name:        name,
		Protocol:    "vmess",
		Address:     host,
		Port:        port,
		Network:     netType,
		Security:    security,
		SNI:         sni,
		Path:        path,
		SubID:       subID,
		RawOutbound: outbound,
	}, nil
}

func parseTrojan(link string, subID string) (*NodeConfig, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	password := u.User.Username()
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(portStr)

	q := u.Query()
	sni := q.Get("sni")
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("Trojan-%s:%d", host, port)
	} else {
		name, _ = url.PathUnescape(name)
	}

	tag := fmt.Sprintf("node-%d", time.Now().UnixNano()%1000000)
	outbound := map[string]interface{}{
		"tag":      tag,
		"protocol": "trojan",
		"settings": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{
					"address":  host,
					"port":     port,
					"password": password,
				},
			},
		},
		"streamSettings": map[string]interface{}{
			"network":  "tcp",
			"security": "tls",
			"tlsSettings": map[string]interface{}{
				"serverName": sni,
			},
		},
	}

	return &NodeConfig{
		ID:          tag,
		Tag:         tag,
		Name:        name,
		Protocol:    "trojan",
		Address:     host,
		Port:        port,
		Network:     "tcp",
		Security:    "tls",
		SNI:         sni,
		SubID:       subID,
		RawOutbound: outbound,
	}, nil
}

func parseShadowsocks(link string, subID string) (*NodeConfig, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	tag := fmt.Sprintf("node-%d", time.Now().UnixNano()%1000000)
	name := u.Fragment
	if name == "" {
		name = fmt.Sprintf("SS-%s", u.Host)
	} else {
		name, _ = url.PathUnescape(name)
	}

	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		return nil, err
	}
	port, _ := strconv.Atoi(portStr)

	method := "aes-128-gcm"
	password := "pass"
	if u.User != nil {
		rawUser := u.User.Username()
		if decoded, err := base64.RawURLEncoding.DecodeString(rawUser); err == nil {
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				method = parts[0]
				password = parts[1]
			}
		}
	}

	outbound := map[string]interface{}{
		"tag":      tag,
		"protocol": "shadowsocks",
		"settings": map[string]interface{}{
			"servers": []interface{}{
				map[string]interface{}{
					"address":  host,
					"port":     port,
					"method":   method,
					"password": password,
				},
			},
		},
	}

	return &NodeConfig{
		ID:          tag,
		Tag:         tag,
		Name:        name,
		Protocol:    "shadowsocks",
		Address:     host,
		Port:        port,
		Network:     "tcp",
		Security:    "none",
		SubID:       subID,
		RawOutbound: outbound,
	}, nil
}

func handleAddSubscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, `{"error":"invalid subscription parameters"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	subID := fmt.Sprintf("sub-%d", time.Now().Unix())
	if req.Name == "" {
		req.Name = fmt.Sprintf("订阅-%d", len(appData.Subscriptions)+1)
	}

	nodes, err := fetchSubscriptionNodes(req.URL, subID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"拉取订阅失败: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	sub := Subscription{
		ID:         subID,
		Name:       req.Name,
		URL:        req.URL,
		LastUpdate: time.Now().Unix(),
		NodeCount:  len(nodes),
	}
	appData.Subscriptions = append(appData.Subscriptions, sub)
	appData.Nodes = append(appData.Nodes, nodes...)

	if appData.ActiveNodeTag == "" && len(appData.Nodes) > 0 {
		appData.ActiveNodeTag = appData.Nodes[0].Tag
	}

	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   len(nodes),
		"sub":     sub,
	})
}

func handleUpdateSubscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var targetSub *Subscription
	for i := range appData.Subscriptions {
		if appData.Subscriptions[i].ID == req.ID {
			targetSub = &appData.Subscriptions[i]
			break
		}
	}
	if targetSub == nil {
		http.Error(w, `{"error":"subscription not found"}`, http.StatusNotFound)
		return
	}

	newNodes, err := fetchSubscriptionNodes(targetSub.URL, targetSub.ID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"更新失败: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Remove old nodes from this subscription
	keptNodes := []NodeConfig{}
	for _, n := range appData.Nodes {
		if n.SubID != targetSub.ID {
			keptNodes = append(keptNodes, n)
		}
	}
	appData.Nodes = append(keptNodes, newNodes...)
	targetSub.LastUpdate = time.Now().Unix()
	targetSub.NodeCount = len(newNodes)

	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   len(newNodes),
	})
}

func handleDeleteSubscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var keptSubs []Subscription
	for _, s := range appData.Subscriptions {
		if s.ID != req.ID {
			keptSubs = append(keptSubs, s)
		}
	}
	var keptNodes []NodeConfig
	for _, n := range appData.Nodes {
		if n.SubID != req.ID {
			keptNodes = append(keptNodes, n)
		}
	}
	appData.Subscriptions = keptSubs
	appData.Nodes = keptNodes

	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func fetchSubscriptionNodes(subURL string, subID string) ([]NodeConfig, error) {
	client := &http.Client{Timeout: 12 * time.Second}
	resp, err := client.Get(subURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Auto Base64 decode
	rawText := strings.TrimSpace(string(body))
	if decoded, err := base64.StdEncoding.DecodeString(rawText); err == nil {
		rawText = string(decoded)
	} else if decoded, err := base64.RawURLEncoding.DecodeString(rawText); err == nil {
		rawText = string(decoded)
	}

	lines := strings.Split(rawText, "\n")
	var nodes []NodeConfig
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if node, err := parseNodeURI(line, subID); err == nil && node != nil {
			nodes = append(nodes, *node)
		}
	}
	return nodes, nil
}

func handleImportNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	lines := strings.Split(req.Text, "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if node, err := parseNodeURI(line, "manual"); err == nil && node != nil {
			appData.Nodes = append(appData.Nodes, *node)
			count++
		}
	}

	if appData.ActiveNodeTag == "" && len(appData.Nodes) > 0 {
		appData.ActiveNodeTag = appData.Nodes[0].Tag
	}

	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   count,
	})
}

func handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var kept []NodeConfig
	for _, n := range appData.Nodes {
		if n.Tag != req.Tag {
			kept = append(kept, n)
		}
	}
	appData.Nodes = kept
	if appData.ActiveNodeTag == req.Tag {
		if len(appData.Nodes) > 0 {
			appData.ActiveNodeTag = appData.Nodes[0].Tag
		} else {
			appData.ActiveNodeTag = ""
		}
	}

	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func handleSetActiveNode(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Tag string `json:"tag"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	appData.ActiveNodeTag = req.Tag
	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "active_tag": req.Tag})
}

func handlePingNodes(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	appData, err := loadData()
	mu.Unlock()
	if err != nil {
		http.Error(w, `{"error":"failed to load data"}`, http.StatusInternalServerError)
		return
	}

	results := make(map[string]int64)
	var wg sync.WaitGroup
	var pmu sync.Mutex

	for _, n := range appData.Nodes {
		wg.Add(1)
		go func(node NodeConfig) {
			defer wg.Done()
			addr := fmt.Sprintf("%s:%d", node.Address, node.Port)
			start := time.Now()
			d := net.Dialer{Timeout: 2 * time.Second}
			conn, err := d.DialContext(context.Background(), "tcp", addr)
			rtt := int64(-1)
			if err == nil {
				rtt = time.Since(start).Milliseconds()
				conn.Close()
			}
			pmu.Lock()
			results[node.Tag] = rtt
			pmu.Unlock()
		}(n)
	}
	wg.Wait()

	mu.Lock()
	for i := range appData.Nodes {
		if rtt, ok := results[appData.Nodes[i].Tag]; ok {
			appData.Nodes[i].PingRTT = rtt
		}
	}
	_ = saveData(appData)
	mu.Unlock()

	_ = json.NewEncoder(w).Encode(results)
}

func handleSaveInbounds(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var inbounds []CustomInbound
	if err := json.NewDecoder(r.Body).Decode(&inbounds); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	appData.Inbounds = inbounds
	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "自定义入站配置已保存"})
}

func handleSaveRouting(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		DomainStrategy string        `json:"domain_strategy"`
		Rules          []RoutingRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	if req.DomainStrategy != "" {
		appData.DomainStrategy = req.DomainStrategy
	}
	appData.Rules = req.Rules
	_ = saveData(appData)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "message": "分流路由规则已保存"})
}

// Generate complete Xray config.json
func generateXrayConfig(appData *AppData) (map[string]interface{}, error) {
	// 1. Inbounds: fixed inbound for hev-socks5-tunnel + custom user inbounds
	inbounds := []interface{}{
		map[string]interface{}{
			"tag":      "hev-socks-in",
			"port":     10808,
			"listen":   "127.0.0.1",
			"protocol": "socks",
			"settings": map[string]interface{}{
				"auth": "noauth",
				"udp":  true,
			},
			"sniffing": map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"routeOnly":    true,
			},
		},
	}

	for _, in := range appData.Inbounds {
		if !in.Enabled {
			continue
		}
		inObj := map[string]interface{}{
			"tag":      in.Tag,
			"port":     in.Port,
			"listen":   in.Listen,
			"protocol": in.Protocol,
		}
		settings := map[string]interface{}{"udp": true}
		if in.Username != "" && in.Password != "" {
			settings["auth"] = "password"
			settings["accounts"] = []interface{}{
				map[string]string{"user": in.Username, "pass": in.Password},
			}
		} else {
			settings["auth"] = "noauth"
		}
		inObj["settings"] = settings

		if in.Sniffing {
			inObj["sniffing"] = map[string]interface{}{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"routeOnly":    true,
			}
		}
		inbounds = append(inbounds, inObj)
	}

	// 2. Outbounds: active node, other referenced nodes, direct, block
	outbounds := []interface{}{}
	nodeMap := make(map[string]NodeConfig)
	for _, n := range appData.Nodes {
		nodeMap[n.Tag] = n
	}

	// Active node as default proxy outbound
	if activeNode, ok := nodeMap[appData.ActiveNodeTag]; ok && activeNode.RawOutbound != nil {
		activeOut := copyMap(activeNode.RawOutbound)
		activeOut["tag"] = "proxy-default"
		outbounds = append(outbounds, activeOut)
	}

	// Other nodes used in routing
	addedTags := map[string]bool{"proxy-default": true}
	for _, r := range appData.Rules {
		if r.Enabled && r.OutboundTag != "" && r.OutboundTag != "direct" && r.OutboundTag != "block" && r.OutboundTag != "proxy-default" {
			if !addedTags[r.OutboundTag] {
				if node, ok := nodeMap[r.OutboundTag]; ok && node.RawOutbound != nil {
					outbounds = append(outbounds, node.RawOutbound)
					addedTags[r.OutboundTag] = true
				}
			}
		}
	}

	// Fallback direct and block
	outbounds = append(outbounds, map[string]interface{}{
		"tag":      "direct",
		"protocol": "freedom",
		"settings": map[string]interface{}{},
	})
	outbounds = append(outbounds, map[string]interface{}{
		"tag":      "block",
		"protocol": "blackhole",
		"settings": map[string]interface{}{},
	})

	// 3. Routing rules
	rules := []interface{}{}
	for _, r := range appData.Rules {
		if !r.Enabled || len(r.Values) == 0 {
			continue
		}
		ruleObj := map[string]interface{}{
			"type": "field",
		}
		targetTag := r.OutboundTag
		if targetTag == "" {
			targetTag = "proxy-default"
		}
		ruleObj["outboundTag"] = targetTag

		if r.Type == "domain" {
			ruleObj["domain"] = r.Values
		} else if r.Type == "ip" {
			ruleObj["ip"] = r.Values
		}
		rules = append(rules, ruleObj)
	}

	// Catch-all rule
	rules = append(rules, map[string]interface{}{
		"type":        "field",
		"network":     "tcp,udp",
		"outboundTag": "proxy-default",
	})

	strategy := appData.DomainStrategy
	if strategy == "" {
		strategy = "AsIs"
	}

	return map[string]interface{}{
		"log": map[string]interface{}{
			"loglevel": "warning",
		},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing": map[string]interface{}{
			"domainStrategy": strategy,
			"rules":          rules,
		},
	}, nil
}

func copyMap(src map[string]interface{}) map[string]interface{} {
	dst := make(map[string]interface{})
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func handlePreviewConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cfg, err := generateXrayConfig(appData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(cfg)
}

func handleApplyConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()
	appData, err := loadData()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if len(appData.Nodes) == 0 {
		http.Error(w, `{"error":"当前未导入任何节点，无法生成代理配置"}`, http.StatusBadRequest)
		return
	}

	cfg, err := generateXrayConfig(appData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cfgJSON, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 1. Write temporary test config
	tmpConfig := "/tmp/xray_test.json"
	if err := os.WriteFile(tmpConfig, cfgJSON, 0644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"写入临时文件失败: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpConfig)

	// 2. Syntax check with xray run -test
	cmd := exec.Command(xrayBin, "run", "-test", "-c", tmpConfig)
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Xray 语法预检失败:\n%s", string(out)),
		})
		return
	}

	// 3. Write target config
	_ = os.MkdirAll("/usr/local/etc/xray", 0755)
	if err := os.WriteFile(configFile, cfgJSON, 0644); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"保存 config.json 失败: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	// 4. Restart service
	_ = exec.Command("service", rcService, "restart").Run()
	_ = exec.Command(fmt.Sprintf("/usr/local/etc/rc.d/%s", rcService), "restart").Run()

	time.Sleep(300 * time.Millisecond)
	pid := getPID()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "配置应用成功，Xray 服务已平滑重启",
		"running": pid > 0,
		"pid":     pid,
	})
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	tmpl, err := template.New("index").Parse(htmlIndex)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}

const htmlIndex = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>OPN-Box | Xray Manager 控制台</title>
  <style>
    :root {
      --bg: #0f172a;
      --card: #1e293b;
      --border: #334155;
      --text: #f8fafc;
      --muted: #94a3b8;
      --primary: #38bdf8;
      --success: #10b981;
      --danger: #ef4444;
      --warning: #f59e0b;
      --code-bg: #0b1120;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.6;
      padding: 1.5rem 1rem;
    }
    .container { max-width: 1040px; margin: 0 auto; }
    header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding-bottom: 1rem;
      border-bottom: 1px solid var(--border);
      margin-bottom: 1.5rem;
      flex-wrap: wrap;
      gap: 1rem;
    }
    h1 { margin: 0; font-size: 1.5rem; color: var(--primary); display: flex; align-items: center; gap: 0.5rem; }
    .status-badge {
      display: inline-flex;
      align-items: center;
      gap: 0.4rem;
      padding: 0.3rem 0.8rem;
      border-radius: 9999px;
      font-size: 0.85rem;
      font-weight: bold;
    }
    .status-on { background: rgba(16, 185, 129, 0.2); color: var(--success); border: 1px solid var(--success); }
    .status-off { background: rgba(239, 68, 68, 0.2); color: var(--danger); border: 1px solid var(--danger); }
    .nav-tabs {
      display: flex;
      gap: 0.5rem;
      border-bottom: 1px solid var(--border);
      margin-bottom: 1.5rem;
      flex-wrap: wrap;
    }
    .tab-btn {
      background: none;
      border: none;
      color: var(--muted);
      padding: 0.65rem 1.2rem;
      font-size: 0.95rem;
      cursor: pointer;
      font-weight: 600;
      border-bottom: 2px solid transparent;
      transition: all 0.2s;
    }
    .tab-btn.active { color: var(--primary); border-bottom-color: var(--primary); }
    .tab-content { display: none; }
    .tab-content.active { display: block; }
    .card {
      background: var(--card);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 1.25rem;
      margin-bottom: 1.25rem;
    }
    .card h2 { margin-top: 0; font-size: 1.15rem; color: var(--text); border-bottom: 1px solid rgba(255,255,255,0.05); padding-bottom: 0.5rem; }
    .btn {
      background: var(--primary);
      color: #0f172a;
      border: none;
      padding: 0.5rem 1rem;
      border-radius: 6px;
      font-weight: 600;
      cursor: pointer;
      transition: opacity 0.2s;
      display: inline-flex;
      align-items: center;
      gap: 0.35rem;
      font-size: 0.875rem;
    }
    .btn:hover { opacity: 0.9; }
    .btn-danger { background: var(--danger); color: #fff; }
    .btn-secondary { background: var(--border); color: #fff; }
    .btn-success { background: var(--success); color: #fff; }
    .btn-outline { background: transparent; border: 1px solid var(--border); color: var(--text); }
    .btn-outline:hover { background: rgba(255,255,255,0.05); }
    input, textarea, select {
      background: var(--code-bg);
      border: 1px solid var(--border);
      color: #fff;
      padding: 0.5rem 0.75rem;
      border-radius: 6px;
      font-size: 0.9rem;
      width: 100%;
    }
    table { width: 100%; border-collapse: collapse; margin-top: 0.5rem; font-size: 0.9rem; }
    th, td { padding: 0.65rem 0.8rem; text-align: left; border-bottom: 1px solid var(--border); }
    th { color: var(--muted); font-size: 0.8rem; text-transform: uppercase; }
    .badge {
      display: inline-block;
      padding: 0.15rem 0.5rem;
      border-radius: 4px;
      font-size: 0.75rem;
      font-weight: 600;
    }
    .badge-protocol { background: #3b82f6; color: #fff; }
    .badge-xhttp { background: #8b5cf6; color: #fff; }
    .badge-tls { background: #10b981; color: #fff; }
    .badge-ping { background: rgba(56,189,248,0.2); color: var(--primary); }
    .ping-good { color: var(--success); }
    .ping-timeout { color: var(--danger); }
    pre {
      background: var(--code-bg);
      border: 1px solid var(--border);
      padding: 0.75rem;
      border-radius: 6px;
      color: #e2e8f0;
      font-family: monospace;
      font-size: 0.85rem;
      max-height: 400px;
      overflow-y: auto;
    }
    .row { display: flex; gap: 1rem; margin-bottom: 0.75rem; }
    .col { flex: 1; }
    .help-text { font-size: 0.8rem; color: var(--muted); margin-top: 0.25rem; }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div>
        <h1>⚡ OPN-Box | Xray Manager</h1>
        <div style="color: var(--muted); font-size: 0.85rem; margin-top: 0.2rem;">
          可视化节点订阅、自定义入站 (LAN Inbounds) 与 7层协议嗅探分流控制器
        </div>
      </div>
      <div style="display: flex; gap: 0.5rem; align-items: center;">
        <span id="serviceStatus" class="status-badge status-off">检测中...</span>
        <button class="btn btn-outline" onclick="loadAll()">刷新</button>
        <button class="btn btn-success" onclick="applyConfig()">🚀 保存并应用</button>
      </div>
    </header>

    <div class="nav-tabs">
      <button class="tab-btn active" onclick="switchTab('nodes')">🌐 节点与订阅</button>
      <button class="tab-btn" onclick="switchTab('inbounds')">🔌 自定义入站 (Inbounds)</button>
      <button class="tab-btn" onclick="switchTab('routing')">🔀 分流路由 (Routing)</button>
      <button class="tab-btn" onclick="switchTab('config')">📄 配置与服务</button>
      <button class="tab-btn" onclick="switchTab('logs')">📜 运行日志</button>
    </div>

    <!-- TAB 1: 节点与订阅 -->
    <div id="tab-nodes" class="tab-content active">
      <div class="card">
        <h2>📥 导入节点与订阅</h2>
        <div class="row">
          <div class="col" style="flex: 2;">
            <input type="text" id="subUrl" placeholder="输入订阅 URL (http:// 或 https://，自动 Base64 解码)" />
          </div>
          <div class="col">
            <input type="text" id="subName" placeholder="订阅备注名称 (选填)" />
          </div>
          <div>
            <button class="btn" onclick="addSubscription()">添加订阅</button>
          </div>
        </div>
        <div style="margin-top: 0.75rem;">
          <textarea id="rawNodeLinks" rows="2" placeholder="或直接粘贴节点分享链接 (支持多行 vless://, vmess://, trojan://, ss:// 原生兼容 xhttp)"></textarea>
          <div style="display:flex; justify-content: flex-end; margin-top: 0.4rem;">
            <button class="btn btn-secondary" onclick="importRawNodes()">批量导入链接</button>
          </div>
        </div>
      </div>

      <div class="card" id="subCard" style="display: none;">
        <h2>📋 我的订阅列表</h2>
        <table id="subTable">
          <thead>
            <tr>
              <th>名称</th>
              <th>链接</th>
              <th>节点数</th>
              <th>更新时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody></tbody>
        </table>
      </div>

      <div class="card">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
          <h2 style="margin: 0; border: none; padding: 0;">🛰️ 节点列表 (<span id="nodeCount">0</span>)</h2>
          <button class="btn btn-outline" onclick="pingAllNodes()">⚡ 一键测延迟</button>
        </div>
        <div class="help-text" style="margin-bottom: 0.5rem;">
          单选按钮即可将该节点指定为<strong>全局默认出站</strong>（所有未命中特定规则的海外流量默认从此节点外出）。
        </div>
        <table id="nodeTable">
          <thead>
            <tr>
              <th width="40">主选</th>
              <th>节点名称</th>
              <th>协议</th>
              <th>地址与端口</th>
              <th>传输 / 模式</th>
              <th>安全层</th>
              <th>延迟</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody></tbody>
        </table>
      </div>
    </div>

    <!-- TAB 2: 自定义入站 (Inbounds) -->
    <div id="tab-inbounds" class="tab-content">
      <div class="card">
        <h2>🔌 局域网接入端口 (Custom Inbounds)</h2>
        <p style="color: var(--muted); font-size: 0.875rem;">
          默认情况下，<strong>hev-socks5-tunnel</strong> 会自动在本地绑定 <code>127.0.0.1:10808</code> 作为透明网关接收入站。<br/>
          在此处您可以<strong>额外开放端口</strong>（例如 SOCKS5 或 HTTP 监听在 <code>0.0.0.0:10809</code>），供家庭/办公局域网内的电视、特定主机或手机临时接入代理。
        </p>

        <div style="display: flex; justify-content: flex-end; margin-bottom: 0.75rem;">
          <button class="btn btn-outline" onclick="addInboundRow()">+ 添加新入站端口</button>
        </div>

        <table id="inboundTable">
          <thead>
            <tr>
              <th width="50">启用</th>
              <th>标签 Tag</th>
              <th>协议</th>
              <th>监听地址</th>
              <th>端口</th>
              <th>7层嗅探</th>
              <th>说明 / 认证</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody id="inboundTbody"></tbody>
        </table>
        <div style="margin-top: 1rem; text-align: right;">
          <button class="btn btn-primary" onclick="saveInbounds()">保存入站配置</button>
        </div>
      </div>
    </div>

    <!-- TAB 3: 分流路由 (Routing) -->
    <div id="tab-routing" class="tab-content">
      <div class="card">
        <h2>🔀 域名与 7 层 SNI 分流规则</h2>
        <p style="color: var(--muted); font-size: 0.875rem;">
          借助 Xray 的 TLS/QUIC 嗅探引擎，即使不同网站解析为同一个 CDN IP，也可以按照真实访问域名精准分流到不同的落地节点！
        </p>

        <div style="display: flex; gap: 0.5rem; margin-bottom: 0.75rem; flex-wrap: wrap;">
          <button class="btn btn-outline" onclick="addTemplateRule('ai')">+ 插入 OpenAI / Claude 模板</button>
          <button class="btn btn-outline" onclick="addTemplateRule('media')">+ 插入 Netflix / Disney 模板</button>
          <button class="btn btn-outline" onclick="addRoutingRow()">+ 添加自定义规则</button>
        </div>

        <table id="routingTable">
          <thead>
            <tr>
              <th width="50">启用</th>
              <th>匹配目标 (Domain / IP / GeoSite)</th>
              <th>指定落地出站 (Outbound)</th>
              <th>说明</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody id="routingTbody"></tbody>
        </table>
        <div style="margin-top: 1rem; text-align: right;">
          <button class="btn btn-primary" onclick="saveRouting()">保存路由规则</button>
        </div>
      </div>
    </div>

    <!-- TAB 4: 配置与服务 -->
    <div id="tab-config" class="tab-content">
      <div class="card">
        <h2>⚙️ Xray 核心服务控制</h2>
        <div style="display: flex; gap: 0.5rem; align-items: center; margin-bottom: 1rem;">
          <button class="btn btn-success" onclick="controlService('start')">启动服务</button>
          <button class="btn btn-danger" onclick="controlService('stop')">停止服务</button>
          <button class="btn btn-secondary" onclick="controlService('restart')">重启服务</button>
          <button class="btn btn-outline" onclick="controlService('test')">语法预检 (xray -test)</button>
        </div>
        <div id="serviceOutput" style="display:none; margin-bottom: 1rem;">
          <pre id="outputPre"></pre>
        </div>
      </div>

      <div class="card">
        <h2>📄 自动合成的 config.json 实时预览</h2>
        <button class="btn btn-outline" style="margin-bottom: 0.5rem;" onclick="refreshConfigPreview()">刷新预览</button>
        <pre id="configPreview">加载中...</pre>
      </div>
    </div>

    <!-- TAB 5: 日志 -->
    <div id="tab-logs" class="tab-content">
      <div class="card">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.5rem;">
          <h2 style="margin: 0; border: none;">📜 Xray 运行日志 (/var/log/xray.log)</h2>
          <button class="btn btn-outline" onclick="loadLogs()">刷新日志</button>
        </div>
        <pre id="logPre">正在读取日志...</pre>
      </div>
    </div>
  </div>

  <script>
    let appData = { nodes: [], subscriptions: [], inbounds: [], rules: [], active_node_tag: "" };

    function switchTab(name) {
      document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
      document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
      event.target.classList.add('active');
      document.getElementById('tab-' + name).classList.add('active');
      if (name === 'logs') loadLogs();
      if (name === 'config') refreshConfigPreview();
    }

    async function loadAll() {
      checkStatus();
      try {
        const res = await fetch('/api/data');
        appData = await res.json();
        renderNodes();
        renderSubs();
        renderInbounds();
        renderRouting();
      } catch (e) {
        console.error("加载数据失败", e);
      }
    }

    async function checkStatus() {
      try {
        const res = await fetch('/api/status');
        const st = await res.json();
        const badge = document.getElementById('serviceStatus');
        if (st.running) {
          badge.className = 'status-badge status-on';
          badge.innerText = '● 运行中 (PID: ' + st.pid + ')';
        } else {
          badge.className = 'status-badge status-off';
          badge.innerText = '○ 已停止';
        }
      } catch (e) {}
    }

    function renderNodes() {
      const tbody = document.querySelector('#nodeTable tbody');
      tbody.innerHTML = '';
      document.getElementById('nodeCount').innerText = appData.nodes.length;

      appData.nodes.forEach(n => {
        const tr = document.createElement('tr');
        const isChecked = (n.tag === appData.active_node_tag) ? 'checked' : '';
        const pingText = n.ping_rtt > 0 ? n.ping_rtt + ' ms' : (n.ping_rtt === -1 ? '超时' : '-');
        const pingClass = n.ping_rtt > 0 ? (n.ping_rtt < 150 ? 'ping-good' : '') : (n.ping_rtt === -1 ? 'ping-timeout' : '');

        tr.innerHTML = '<td><input type="radio" name="activeNode" value="' + n.tag + '" ' + isChecked + ' onchange="setActiveNode(\'' + n.tag + '\')" /></td>' +
          '<td><strong>' + escapeHtml(n.name) + '</strong></td>' +
          '<td><span class="badge badge-protocol">' + n.protocol + '</span></td>' +
          '<td><code>' + n.address + ':' + n.port + '</code></td>' +
          '<td><span class="badge badge-xhttp">' + n.network + (n.xhttp_mode ? ' (' + n.xhttp_mode + ')' : '') + '</span></td>' +
          '<td><span class="badge badge-tls">' + n.security + '</span></td>' +
          '<td class="' + pingClass + '">' + pingText + '</td>' +
          '<td><button class="btn btn-outline" style="padding: 0.2rem 0.5rem; font-size: 0.75rem;" onclick="deleteNode(\'' + n.tag + '\')">删除</button></td>';
        tbody.appendChild(tr);
      });
    }

    function renderSubs() {
      const card = document.getElementById('subCard');
      const tbody = document.querySelector('#subTable tbody');
      tbody.innerHTML = '';
      if (!appData.subscriptions || appData.subscriptions.length === 0) {
        card.style.display = 'none';
        return;
      }
      card.style.display = 'block';
      appData.subscriptions.forEach(s => {
        const tr = document.createElement('tr');
        const dateStr = s.last_update ? new Date(s.last_update * 1000).toLocaleString() : '-';
        tr.innerHTML = '<td><strong>' + escapeHtml(s.name) + '</strong></td>' +
          '<td><code style="font-size: 0.75rem;">' + escapeHtml(s.url.substring(0, 45)) + '...</code></td>' +
          '<td>' + (s.node_count || 0) + '</td>' +
          '<td>' + dateStr + '</td>' +
          '<td>' +
            '<button class="btn btn-outline" style="padding: 0.2rem 0.5rem; font-size: 0.75rem;" onclick="updateSub(\'' + s.id + '\')">更新</button> ' +
            '<button class="btn btn-outline" style="padding: 0.2rem 0.5rem; font-size: 0.75rem; color: var(--danger);" onclick="deleteSub(\'' + s.id + '\')">删除</button>' +
          '</td>';
        tbody.appendChild(tr);
      });
    }

    function renderInbounds() {
      const tbody = document.getElementById('inboundTbody');
      tbody.innerHTML = '';
      (appData.inbounds || []).forEach((inb, idx) => {
        const tr = document.createElement('tr');
        tr.innerHTML = '<td><input type="checkbox" ' + (inb.enabled ? 'checked' : '') + ' onchange="appData.inbounds[' + idx + '].enabled = this.checked" /></td>' +
          '<td><input type="text" value="' + escapeHtml(inb.tag) + '" onchange="appData.inbounds[' + idx + '].tag = this.value" /></td>' +
          '<td>' +
            '<select onchange="appData.inbounds[' + idx + '].protocol = this.value">' +
              '<option value="socks" ' + (inb.protocol==='socks'?'selected':'') + '>SOCKS5</option>' +
              '<option value="http" ' + (inb.protocol==='http'?'selected':'') + '>HTTP</option>' +
            '</select>' +
          '</td>' +
          '<td><input type="text" value="' + escapeHtml(inb.listen) + '" onchange="appData.inbounds[' + idx + '].listen = this.value" /></td>' +
          '<td><input type="number" style="width: 90px;" value="' + inb.port + '" onchange="appData.inbounds[' + idx + '].port = parseInt(this.value)" /></td>' +
          '<td><input type="checkbox" ' + (inb.sniffing ? 'checked' : '') + ' onchange="appData.inbounds[' + idx + '].sniffing = this.checked" /></td>' +
          '<td><input type="text" placeholder="说明备注" value="' + escapeHtml(inb.comment||'') + '" onchange="appData.inbounds[' + idx + '].comment = this.value" /></td>' +
          '<td><button class="btn btn-outline" style="padding: 0.2rem 0.5rem; color: var(--danger);" onclick="removeInbound(' + idx + ')">删除</button></td>';
        tbody.appendChild(tr);
      });
    }

    function addInboundRow() {
      if (!appData.inbounds) appData.inbounds = [];
      const newPort = 10811 + appData.inbounds.length;
      appData.inbounds.push({
        id: "in-" + Date.now(),
        tag: "in-custom-" + newPort,
        protocol: "socks",
        listen: "0.0.0.0",
        port: newPort,
        enabled: true,
        sniffing: true,
        comment: "自定义局域网入站"
      });
      renderInbounds();
    }

    function removeInbound(idx) {
      appData.inbounds.splice(idx, 1);
      renderInbounds();
    }

    async function saveInbounds() {
      const res = await fetch('/api/inbounds/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(appData.inbounds)
      });
      const data = await res.json();
      alert(data.message || (data.success ? "入站已保存" : "保存失败"));
    }

    function renderRouting() {
      const tbody = document.getElementById('routingTbody');
      tbody.innerHTML = '';
      (appData.rules || []).forEach((rule, idx) => {
        const tr = document.createElement('tr');
        let options = '<option value="proxy-default" ' + (rule.outbound_tag === 'proxy-default' ? 'selected' : '') + '>★ 默认主代理节点</option>';
        options += '<option value="direct" ' + (rule.outbound_tag === 'direct' ? 'selected' : '') + '>直连 (direct)</option>';
        options += '<option value="block" ' + (rule.outbound_tag === 'block' ? 'selected' : '') + '>拦截 (block)</option>';
        appData.nodes.forEach(n => {
          options += '<option value="' + n.tag + '" ' + (rule.outbound_tag === n.tag ? 'selected' : '') + '>节点: ' + escapeHtml(n.name) + '</option>';
        });

        tr.innerHTML = '<td><input type="checkbox" ' + (rule.enabled ? 'checked' : '') + ' onchange="appData.rules[' + idx + '].enabled = this.checked" /></td>' +
          '<td><input type="text" value="' + escapeHtml((rule.values || []).join(', ')) + '" onchange="appData.rules[' + idx + '].values = this.value.split(\',\').map(s=>s.trim()).filter(s=>s)" /></td>' +
          '<td><select onchange="appData.rules[' + idx + '].outbound_tag = this.value">' + options + '</select></td>' +
          '<td><input type="text" value="' + escapeHtml(rule.comment || '') + '" onchange="appData.rules[' + idx + '].comment = this.value" /></td>' +
          '<td><button class="btn btn-outline" style="padding: 0.2rem 0.5rem; color: var(--danger);" onclick="removeRouting(' + idx + ')">删除</button></td>';
        tbody.appendChild(tr);
      });
    }

    function addRoutingRow() {
      if (!appData.rules) appData.rules = [];
      appData.rules.push({
        id: "rule-" + Date.now(),
        type: "domain",
        values: ["geosite:google"],
        outbound_tag: "proxy-default",
        enabled: true,
        comment: "自定义域名分流"
      });
      renderRouting();
    }

    function addTemplateRule(tpl) {
      if (!appData.rules) appData.rules = [];
      if (tpl === 'ai') {
        appData.rules.unshift({
          id: "rule-ai-" + Date.now(),
          type: "domain",
          values: ["geosite:openai", "geosite:anthropic", "domain:claude.ai", "domain:oaistatic.com"],
          outbound_tag: "proxy-default",
          enabled: true,
          comment: "OpenAI / Claude AI 专线"
        });
      } else if (tpl === 'media') {
        appData.rules.unshift({
          id: "rule-media-" + Date.now(),
          type: "domain",
          values: ["geosite:netflix", "geosite:disney", "geosite:youtube"],
          outbound_tag: "proxy-default",
          enabled: true,
          comment: "海外流媒体 (Netflix / Disney / YouTube)"
        });
      }
      renderRouting();
    }

    function removeRouting(idx) {
      appData.rules.splice(idx, 1);
      renderRouting();
    }

    async function saveRouting() {
      const res = await fetch('/api/routing/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain_strategy: "AsIs", rules: appData.rules })
      });
      const data = await res.json();
      alert(data.message || (data.success ? "分流路由规则已保存" : "保存失败"));
    }

    async function addSubscription() {
      const url = document.getElementById('subUrl').value.trim();
      const name = document.getElementById('subName').value.trim();
      if (!url) return alert('请输入订阅 URL');
      const res = await fetch('/api/subscription/add', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url, name })
      });
      const data = await res.json();
      if (data.success) {
        alert('订阅成功，共导入 ' + data.count + ' 个节点');
        document.getElementById('subUrl').value = '';
        document.getElementById('subName').value = '';
        loadAll();
      } else {
        alert(data.error || '添加订阅失败');
      }
    }

    async function updateSub(id) {
      const res = await fetch('/api/subscription/update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      });
      const data = await res.json();
      if (data.success) {
        alert('更新完毕，当前节点数: ' + data.count);
        loadAll();
      } else {
        alert(data.error || '更新失败');
      }
    }

    async function deleteSub(id) {
      if (!confirm('确定删除该订阅及对应节点？')) return;
      await fetch('/api/subscription/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      });
      loadAll();
    }

    async function importRawNodes() {
      const text = document.getElementById('rawNodeLinks').value.trim();
      if (!text) return alert('请粘贴节点分享链接');
      const res = await fetch('/api/nodes/import', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text })
      });
      const data = await res.json();
      if (data.success) {
        alert('成功导入 ' + data.count + ' 个节点');
        document.getElementById('rawNodeLinks').value = '';
        loadAll();
      } else {
        alert(data.error || '导入失败');
      }
    }

    async function deleteNode(tag) {
      if (!confirm('确定删除该节点？')) return;
      await fetch('/api/nodes/delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tag })
      });
      loadAll();
    }

    async function setActiveNode(tag) {
      appData.active_node_tag = tag;
      await fetch('/api/nodes/active', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tag })
      });
    }

    async function pingAllNodes() {
      const btn = event.target;
      btn.innerText = '⏳ 测速中...';
      try {
        const res = await fetch('/api/nodes/ping', { method: 'POST' });
        loadAll();
      } finally {
        btn.innerText = '⚡ 一键测延迟';
      }
    }

    async function applyConfig() {
      const res = await fetch('/api/apply', { method: 'POST' });
      const data = await res.json();
      if (data.success) {
        alert(data.message);
        checkStatus();
      } else {
        alert("应用失败:\n" + (data.error || "未知错误"));
      }
    }

    async function controlService(action) {
      const res = await fetch('/api/service', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action })
      });
      const data = await res.json();
      const div = document.getElementById('serviceOutput');
      const pre = document.getElementById('outputPre');
      div.style.display = 'block';
      pre.innerText = data.output || (data.success ? '操作执行成功' : '操作失败');
      checkStatus();
    }

    async function refreshConfigPreview() {
      const pre = document.getElementById('configPreview');
      pre.innerText = '生成中...';
      const res = await fetch('/api/config/preview');
      const data = await res.json();
      pre.innerText = JSON.stringify(data, null, 2);
    }

    async function loadLogs() {
      const pre = document.getElementById('logPre');
      const res = await fetch('/api/logs?lines=100');
      const data = await res.json();
      pre.innerText = (data.logs || []).join('\n');
    }

    function escapeHtml(str) {
      if (!str) return '';
      return str.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
    }

    // Init
    loadAll();
    setInterval(checkStatus, 5000);
  </script>
</body>
</html>
`
