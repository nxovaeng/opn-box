package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// HevConfig represents the hev-socks5-tunnel configuration schema
type HevConfig struct {
	Tunnel TunnelSection `yaml:"tunnel" json:"tunnel"`
	Socks5 Socks5Section `yaml:"socks5" json:"socks5"`
	Misc   MiscSection   `yaml:"misc,omitempty" json:"misc,omitempty"`
}

type TunnelSection struct {
	Name       string `yaml:"name" json:"name"`
	MTU        int    `yaml:"mtu" json:"mtu"`
	MultiQueue bool   `yaml:"multi-queue,omitempty" json:"multi_queue"`
	IPv4       string `yaml:"ipv4" json:"ipv4"`
	IPv6       string `yaml:"ipv6,omitempty" json:"ipv6"`
}

type Socks5Section struct {
	Address  string `yaml:"address" json:"address"`
	Port     int    `yaml:"port" json:"port"`
	UDP      string `yaml:"udp,omitempty" json:"udp"`
	Username string `yaml:"username,omitempty" json:"username"`
	Password string `yaml:"password,omitempty" json:"password"`
}

type MiscSection struct {
	TaskStackSize    int    `yaml:"task-stack-size,omitempty" json:"task_stack_size"`
	ConnectTimeout   int    `yaml:"connect-timeout,omitempty" json:"connect_timeout"`
	ReadWriteTimeout int    `yaml:"read-write-timeout,omitempty" json:"read_write_timeout"`
	LogFile          string `yaml:"log-file,omitempty" json:"log_file"`
	LogLevel         string `yaml:"log-level,omitempty" json:"log_level"`
}

var (
	configFile string
	logFile    string
	rcService  string
	listenAddr string
	mu         sync.Mutex
)

func main() {
	flag.StringVar(&listenAddr, "listen", ":5382", "HTTP server listen address")
	flag.StringVar(&configFile, "config", "/usr/local/etc/hev-socks5-tunnel/config.yaml", "Path to hev-socks5-tunnel config.yaml")
	flag.StringVar(&logFile, "log-file", "/var/log/hev-socks5-tunnel.log", "Path to hev-socks5-tunnel log file")
	flag.StringVar(&rcService, "rc-service", "hev_socks5_tunnel", "FreeBSD rc.d service name")
	flag.Parse()

	http.HandleFunc("/", handleIndex)
	http.HandleFunc("/api/status", handleStatus)
	http.HandleFunc("/api/config", handleConfig)
	http.HandleFunc("/api/service", handleService)
	http.HandleFunc("/api/logs", handleLogs)

	log.Printf("[hev-controller] Starting Tun2Socks Web Manager on %s", listenAddr)
	log.Printf("[hev-controller] Target config: %s, log: %s", configFile, logFile)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("[hev-controller] Failed to start server: %v", err)
	}
}

// ServiceStatus represents the current runtime status
type ServiceStatus struct {
	Running   bool       `json:"running"`
	PID       int        `json:"pid"`
	Config    *HevConfig `json:"config"`
	Error     string     `json:"error,omitempty"`
	Timestamp int64      `json:"timestamp"`
}

func getPID() int {
	// 1. Try pgrep hev-socks5-tunnel
	out, err := exec.Command("pgrep", "-x", "hev-socks5-tunnel").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 {
			if pid, err := strconv.Atoi(lines[0]); err == nil && pid > 0 {
				return pid
			}
		}
	}
	// 2. Try pidfile if exists
	pidBytes, err := os.ReadFile("/var/run/hev-socks5-tunnel.pid")
	if err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes))); err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

func loadConfig() (*HevConfig, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		// Provide fallback default config
		return &HevConfig{
			Tunnel: TunnelSection{Name: "tun0", MTU: 1500, IPv4: "198.18.0.1"},
			Socks5: Socks5Section{Address: "127.0.0.1", Port: 10808, UDP: "udp"},
			Misc:   MiscSection{LogFile: logFile, LogLevel: "info"},
		}, err
	}
	var cfg HevConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func saveConfig(cfg *HevConfig) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	// Ensure directory exists
	dir := "/usr/local/etc/hev-socks5-tunnel"
	if idx := strings.LastIndex(configFile, "/"); idx != -1 {
		dir = configFile[:idx]
	}
	_ = os.MkdirAll(dir, 0755)

	return os.WriteFile(configFile, data, 0644)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	pid := getPID()
	cfg, err := loadConfig()
	status := ServiceStatus{
		Running:   pid > 0,
		PID:       pid,
		Config:    cfg,
		Timestamp: time.Now().Unix(),
	}
	if err != nil {
		status.Error = err.Error()
	}
	_ = json.NewEncoder(w).Encode(status)
}

func handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	mu.Lock()
	defer mu.Unlock()

	if r.Method == http.MethodGet {
		cfg, err := loadConfig()
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			HevConfig
			Restart bool `json:"restart"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"invalid json: %s"}`, err.Error()), http.StatusBadRequest)
			return
		}

		if err := saveConfig(&req.HevConfig); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"failed to save config: %s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		if req.Restart {
			_ = exec.Command("service", rcService, "restart").Run()
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "配置已保存",
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
		Action string `json:"action"` // start, stop, restart
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	validActions := map[string]bool{"start": true, "stop": true, "restart": true, "status": true}
	if !validActions[req.Action] {
		http.Error(w, `{"error":"invalid action"}`, http.StatusBadRequest)
		return
	}

	// Try service <rcService> <action>, or /usr/local/etc/rc.d/<rcService> <action>
	cmd := exec.Command("service", rcService, req.Action)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback to explicit path
		rcPath := fmt.Sprintf("/usr/local/etc/rc.d/%s", rcService)
		cmd = exec.Command(rcPath, req.Action)
		out, err = cmd.CombinedOutput()
	}

	time.Sleep(300 * time.Millisecond)
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
	linesToRead := 60
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

	// Read last lines using tail-like approach
	data, _ := io.ReadAll(file)
	allLines := strings.Split(string(data), "\n")
	start := 0
	if len(allLines) > linesToRead {
		start = len(allLines) - linesToRead
	}
	recent := allLines[start:]

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": recent,
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
  <title>Tun2Socks 极速转发管理面板 - hev-socks5-tunnel</title>
  <style>
    :root {
      --bg: #0b1120;
      --card-bg: #1e293b;
      --border: #334155;
      --text: #f8fafc;
      --text-muted: #94a3b8;
      --primary: #38bdf8;
      --primary-hover: #0284c7;
      --success: #22c55e;
      --danger: #ef4444;
      --code-bg: #030712;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", sans-serif;
      background: var(--bg);
      color: var(--text);
      line-height: 1.5;
      padding: 2rem 1rem;
    }
    .container { max-width: 960px; margin: 0 auto; }
    header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 2rem;
      border-bottom: 1px solid var(--border);
      padding-bottom: 1.25rem;
    }
    h1 { font-size: 1.5rem; color: var(--primary); display: flex; align-items: center; gap: 0.5rem; }
    .badge {
      font-size: 0.75rem;
      padding: 0.25rem 0.6rem;
      border-radius: 9999px;
      font-weight: 600;
      background: rgba(56, 189, 248, 0.15);
      color: var(--primary);
      border: 1px solid rgba(56, 189, 248, 0.3);
    }
    .badge.active { background: rgba(34, 197, 94, 0.2); color: var(--success); border-color: rgba(34, 197, 94, 0.4); }
    .badge.stopped { background: rgba(239, 68, 68, 0.2); color: var(--danger); border-color: rgba(239, 68, 68, 0.4); }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 1.25rem; margin-bottom: 1.5rem; }
    .card {
      background: var(--card-bg);
      border: 1px solid var(--border);
      border-radius: 10px;
      padding: 1.25rem;
    }
    .card h2 { font-size: 1.1rem; margin-bottom: 1rem; color: var(--text); border-bottom: 1px solid var(--border); padding-bottom: 0.5rem; }
    .stat-label { font-size: 0.8rem; color: var(--text-muted); text-transform: uppercase; margin-bottom: 0.25rem; }
    .stat-value { font-size: 1.1rem; font-weight: 600; color: #fff; }
    .btn {
      padding: 0.5rem 1rem;
      border-radius: 6px;
      font-size: 0.875rem;
      font-weight: 600;
      cursor: pointer;
      border: none;
      transition: all 0.2s;
    }
    .btn-primary { background: var(--primary); color: #000; }
    .btn-primary:hover { background: var(--primary-hover); }
    .btn-success { background: var(--success); color: #000; }
    .btn-danger { background: var(--danger); color: #fff; }
    .btn-outline { background: transparent; border: 1px solid var(--border); color: var(--text); }
    .btn-outline:hover { background: var(--border); }
    .actions { display: flex; gap: 0.5rem; margin-top: 1rem; }
    .form-group { margin-bottom: 1rem; }
    label { display: block; font-size: 0.85rem; color: var(--text-muted); margin-bottom: 0.4rem; }
    input, select {
      width: 100%;
      padding: 0.6rem 0.8rem;
      border-radius: 6px;
      background: var(--code-bg);
      border: 1px solid var(--border);
      color: #fff;
      font-size: 0.9rem;
      font-family: inherit;
    }
    input:focus, select:focus { outline: none; border-color: var(--primary); }
    .log-box {
      background: var(--code-bg);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 0.75rem;
      height: 240px;
      overflow-y: auto;
      font-family: ui-monospace, SFMono-Regular, monospace;
      font-size: 0.8rem;
      color: #cbd5e1;
      white-space: pre-wrap;
    }
    .info-callout {
      background: rgba(56, 189, 248, 0.08);
      border-left: 4px solid var(--primary);
      padding: 0.75rem 1rem;
      border-radius: 0 6px 6px 0;
      font-size: 0.85rem;
      margin-bottom: 1.25rem;
    }
    .notice { font-size: 0.8rem; color: var(--text-muted); margin-top: 0.5rem; }
  </style>
</head>
<body>
  <div class="container">
    <header>
      <div>
        <h1>⚡ hev-socks5-tunnel 管理面板</h1>
        <p style="color: var(--text-muted); font-size: 0.85rem; margin-top: 0.25rem;">
          高性能 C 语言 / LwIP / kqueue 架构 · OPNsense 策略路由专享
        </p>
      </div>
      <div>
        <span id="serviceBadge" class="badge">检测中...</span>
      </div>
    </header>

    <div class="info-callout">
      <strong>💡 架构工作原理：</strong> MosDNS 解析代理域名 ➔ pf-aliasd 注入 OPNsense &lt;GFW_Proxy&gt; 防火墙表 ➔ 防火墙策略路由引流至 <code>tun0</code> ➔ hev-socks5-tunnel 极速封装为 SOCKS5 转发给本地 Xray (xhttp 节点)。
    </div>

    <!-- 状态概览网格 -->
    <div class="grid">
      <div class="card">
        <h2>服务状态</h2>
        <div style="display: flex; justify-content: space-between; align-items: center;">
          <div>
            <div class="stat-label">运行状态</div>
            <div id="statStatus" class="stat-value">--</div>
          </div>
          <div>
            <div class="stat-label">PID 进程号</div>
            <div id="statPid" class="stat-value">--</div>
          </div>
        </div>
        <div class="actions">
          <button id="btnStart" class="btn btn-success" onclick="controlService('start')">启动</button>
          <button id="btnStop" class="btn btn-danger" onclick="controlService('stop')">停止</button>
          <button id="btnRestart" class="btn btn-primary" onclick="controlService('restart')">重启</button>
        </div>
      </div>

      <div class="card">
        <h2>虚拟网卡 (TUN)</h2>
        <div style="display: flex; justify-content: space-between;">
          <div>
            <div class="stat-label">接口名称</div>
            <div id="statTunName" class="stat-value">tun0</div>
          </div>
          <div>
            <div class="stat-label">虚拟 IP</div>
            <div id="statTunIp" class="stat-value">198.18.0.1</div>
          </div>
          <div>
            <div class="stat-label">MTU</div>
            <div id="statTunMtu" class="stat-value">1500</div>
          </div>
        </div>
        <p class="notice">在 OPNsense 中分配该接口，并将 &lt;GFW_Proxy&gt; 流量以此为网关出站。</p>
      </div>

      <div class="card">
        <h2>上游 SOCKS5 出站目标</h2>
        <div style="display: flex; justify-content: space-between;">
          <div>
            <div class="stat-label">服务器地址</div>
            <div id="statSocksAddr" class="stat-value">127.0.0.1</div>
          </div>
          <div>
            <div class="stat-label">端口</div>
            <div id="statSocksPort" class="stat-value">10808</div>
          </div>
          <div>
            <div class="stat-label">UDP 转发</div>
            <div id="statSocksUdp" class="stat-value">启用 (udp)</div>
          </div>
        </div>
        <p class="notice">完美对接本地运行的 Xray/V2Ray (VLESS+xhttp+CDN 节点)。</p>
      </div>
    </div>

    <!-- 配置表单 -->
    <div class="card" style="margin-bottom: 1.5rem;">
      <h2>⚙️ 转发参数配置</h2>
      <form id="configForm" onsubmit="saveSettings(event)">
        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 1rem;">
          <div class="form-group">
            <label>上游 SOCKS5 地址</label>
            <input type="text" id="cfgSocksAddr" required placeholder="127.0.0.1">
          </div>
          <div class="form-group">
            <label>上游 SOCKS5 端口</label>
            <input type="number" id="cfgSocksPort" required placeholder="10808">
          </div>
        </div>

        <div style="display: grid; grid-template-columns: 1fr 1fr; gap: 1rem;">
          <div class="form-group">
            <label>TUN 接口名称 (FreeBSD)</label>
            <input type="text" id="cfgTunName" required placeholder="tun0">
          </div>
          <div class="form-group">
            <label>TUN IPv4 地址</label>
            <input type="text" id="cfgTunIpv4" required placeholder="198.18.0.1">
          </div>
        </div>

        <div style="display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 1rem;">
          <div class="form-group">
            <label>MTU 大小</label>
            <input type="number" id="cfgTunMtu" value="1500">
          </div>
          <div class="form-group">
            <label>UDP 转发模式</label>
            <select id="cfgSocksUdp">
              <option value="udp">原生 UDP (SOCKS5 UDP Associate)</option>
              <option value="tcp">UDP over TCP</option>
            </select>
          </div>
          <div class="form-group">
            <label>日志级别</label>
            <select id="cfgLogLevel">
              <option value="info">Info</option>
              <option value="warn">Warn</option>
              <option value="error">Error</option>
              <option value="debug">Debug</option>
            </select>
          </div>
        </div>

        <div style="display: flex; justify-content: flex-end; gap: 0.75rem; margin-top: 0.5rem;">
          <button type="submit" class="btn btn-primary">💾 保存并立即重启服务</button>
        </div>
      </form>
    </div>

    <!-- 运行日志 -->
    <div class="card">
      <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 0.75rem;">
        <h2>📋 实时运行日志</h2>
        <div>
          <button class="btn btn-outline" style="padding: 0.25rem 0.6rem; font-size: 0.75rem;" onclick="refreshLogs()">刷新日志</button>
        </div>
      </div>
      <div id="logBox" class="log-box">正在加载日志...</div>
    </div>
  </div>

  <script>
    async function fetchStatus() {
      try {
        const res = await fetch('/api/status');
        const data = await res.json();
        const isRunning = data.running;

        const badge = document.getElementById('serviceBadge');
        const statStatus = document.getElementById('statStatus');
        const statPid = document.getElementById('statPid');

        if (isRunning) {
          badge.className = 'badge active';
          badge.textContent = '🟢 运行中';
          statStatus.textContent = '运行中';
          statStatus.style.color = 'var(--success)';
          statPid.textContent = data.pid;
        } else {
          badge.className = 'badge stopped';
          badge.textContent = '🔴 已停止';
          statStatus.textContent = '已停止';
          statStatus.style.color = 'var(--danger)';
          statPid.textContent = '--';
        }

        if (data.config) {
          const c = data.config;
          document.getElementById('statTunName').textContent = c.tunnel?.name || 'tun0';
          document.getElementById('statTunIp').textContent = c.tunnel?.ipv4 || '198.18.0.1';
          document.getElementById('statTunMtu').textContent = c.tunnel?.mtu || 1500;
          document.getElementById('statSocksAddr').textContent = c.socks5?.address || '127.0.0.1';
          document.getElementById('statSocksPort').textContent = c.socks5?.port || 10808;
          document.getElementById('statSocksUdp').textContent = (c.socks5?.udp === 'udp' ? '原生 UDP' : c.socks5?.udp) || '启用';

          // 填充表单
          document.getElementById('cfgSocksAddr').value = c.socks5?.address || '127.0.0.1';
          document.getElementById('cfgSocksPort').value = c.socks5?.port || 10808;
          document.getElementById('cfgTunName').value = c.tunnel?.name || 'tun0';
          document.getElementById('cfgTunIpv4').value = c.tunnel?.ipv4 || '198.18.0.1';
          document.getElementById('cfgTunMtu').value = c.tunnel?.mtu || 1500;
          document.getElementById('cfgSocksUdp').value = c.socks5?.udp || 'udp';
          document.getElementById('cfgLogLevel').value = c.misc?.log_level || 'info';
        }
      } catch (e) {
        console.error('Failed to fetch status:', e);
      }
    }

    async function controlService(action) {
      if (!confirm('确定执行 ' + action + ' 服务操作吗？')) return;
      try {
        const res = await fetch('/api/service', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action })
        });
        const result = await res.json();
        alert(result.success ? '操作成功' : '操作失败: ' + (result.output || '未知错误'));
        setTimeout(fetchStatus, 500);
        setTimeout(refreshLogs, 1000);
      } catch (e) {
        alert('请求失败: ' + e);
      }
    }

    async function saveSettings(e) {
      e.preventDefault();
      const payload = {
        tunnel: {
          name: document.getElementById('cfgTunName').value.trim(),
          ipv4: document.getElementById('cfgTunIpv4').value.trim(),
          mtu: parseInt(document.getElementById('cfgTunMtu').value, 10) || 1500
        },
        socks5: {
          address: document.getElementById('cfgSocksAddr').value.trim(),
          port: parseInt(document.getElementById('cfgSocksPort').value, 10) || 10808,
          udp: document.getElementById('cfgSocksUdp').value
        },
        misc: {
          log_file: '/var/log/hev-socks5-tunnel.log',
          log_level: document.getElementById('cfgLogLevel').value
        },
        restart: true
      };

      try {
        const res = await fetch('/api/config', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload)
        });
        const data = await res.json();
        if (data.success) {
          alert('配置保存成功，并已触发服务重启！');
          setTimeout(fetchStatus, 800);
          setTimeout(refreshLogs, 1200);
        } else {
          alert('保存失败: ' + data.error);
        }
      } catch (err) {
        alert('保存错误: ' + err);
      }
    }

    async function refreshLogs() {
      try {
        const res = await fetch('/api/logs?lines=80');
        const data = await res.json();
        const logBox = document.getElementById('logBox');
        if (data.logs && data.logs.length) {
          logBox.textContent = data.logs.join('\n');
          logBox.scrollTop = logBox.scrollHeight;
        } else {
          logBox.textContent = '暂无日志输出';
        }
      } catch (e) {
        console.error('Failed to load logs:', e);
      }
    }

    // 初始化
    fetchStatus();
    refreshLogs();
    setInterval(fetchStatus, 5000);
    setInterval(refreshLogs, 10000);
  </script>
</body>
</html>
`
