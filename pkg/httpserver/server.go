package httpserver

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Config configures the dual HTTP and HTTPS server
type Config struct {
	ListenAddr  string
	TLSCertFile string
	TLSKeyFile  string
	ServiceName string
}

type dualListener struct {
	net.Listener
	tlsConfig *tls.Config
}

type peekConn struct {
	net.Conn
	r io.Reader
}

func (c *peekConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (l *dualListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}

	// Read 1 byte with a 3-second timeout to detect TLS ClientHello (0x16)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	var buf [1]byte
	n, err := c.Read(buf[:])
	_ = c.SetReadDeadline(time.Time{})

	if err != nil {
		_ = c.Close()
		return nil, err
	}

	prefixed := &peekConn{
		Conn: c,
		r:    io.MultiReader(bytes.NewReader(buf[:n]), c),
	}

	// 0x16 is the first byte of a TLS Handshake record (ClientHello)
	if buf[0] == 0x16 && l.tlsConfig != nil {
		return tls.Server(prefixed, l.tlsConfig), nil
	}

	// Plain HTTP connection
	return prefixed, nil
}

// certManager handles dynamic loading and cache-invalidation of certificates
type certManager struct {
	certFile string
	keyFile  string
	mu       sync.RWMutex
	lastMod  time.Time
	cached   *tls.Certificate
	fallback *tls.Certificate
}

func newCertManager(certFile, keyFile string) *certManager {
	cm := &certManager{
		certFile: certFile,
		keyFile:  keyFile,
	}
	cm.fallback = generateSelfSignedCert()
	_ = cm.reload()
	return cm
}

func (cm *certManager) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	cm.mu.RLock()
	cert := cm.cached
	cm.mu.RUnlock()

	// Periodically check if cert on disk has changed
	if cm.certFile != "" {
		if fi, err := os.Stat(cm.certFile); err == nil && fi.ModTime().After(cm.lastMod) {
			_ = cm.reload()
			cm.mu.RLock()
			cert = cm.cached
			cm.mu.RUnlock()
		}
	}

	if cert != nil {
		return cert, nil
	}
	return cm.fallback, nil
}

func (cm *certManager) reload() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if cm.certFile == "" {
		return nil
	}

	keyPath := cm.keyFile
	if keyPath == "" {
		keyPath = cm.certFile
	}

	fi, err := os.Stat(cm.certFile)
	if err != nil {
		return err
	}

	// Try loading combined certificate & private key (e.g. OPNsense /var/etc/cert.pem)
	cert, err := tls.LoadX509KeyPair(cm.certFile, keyPath)
	if err != nil {
		log.Printf("[httpserver] Warning: failed to load certificate from %s / %s: %v", cm.certFile, keyPath, err)
		return err
	}

	cm.cached = &cert
	cm.lastMod = fi.ModTime()
	log.Printf("[httpserver] Successfully loaded TLS certificate from %s", cm.certFile)
	return nil
}

// generateSelfSignedCert generates an in-memory fallback self-signed certificate
func generateSelfSignedCert() *tls.Certificate {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil
	}

	serialNumber, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	tmpl := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "opn-box-internal",
			Organization: []string{"OPN-Box Project"},
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost", "opnsense.local", "router.local"},
		IPAddresses: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("::1"),
		},
	}

	// Collect local IP addresses to add to SANs
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ipnet.IP)
			}
		}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil
	}

	certPem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil
	}
	keyPem := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	cert, err := tls.X509KeyPair(certPem, keyPem)
	if err != nil {
		return nil
	}
	return &cert
}

// ListenAndServeDual starts a dual HTTP and HTTPS server on the given address
func ListenAndServeDual(cfg Config, handler http.Handler) error {
	if cfg.TLSCertFile == "" {
		// Default to OPNsense standard certificate path
		if _, err := os.Stat("/var/etc/cert.pem"); err == nil {
			cfg.TLSCertFile = "/var/etc/cert.pem"
			if cfg.TLSKeyFile == "" {
				cfg.TLSKeyFile = "/var/etc/cert.pem"
			}
		}
	}

	cm := newCertManager(cfg.TLSCertFile, cfg.TLSKeyFile)

	tlsConfig := &tls.Config{
		GetCertificate: cm.getCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	tcpLn, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", cfg.ListenAddr, err)
	}

	dualLn := &dualListener{
		Listener:  tcpLn,
		tlsConfig: tlsConfig,
	}

	// Wrap handler to allow iframe embedding from OPNsense WebGUI
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ensure headers allow embedding in iframes
		w.Header().Del("X-Frame-Options")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		handler.ServeHTTP(w, r)
	})

	srv := &http.Server{
		Handler:      wrappedHandler,
		ReadTimeout:  120 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  180 * time.Second,
	}

	certInfo := "in-memory self-signed fallback"
	if cm.cached != nil {
		certInfo = cfg.TLSCertFile
	}
	log.Printf("[%s] Server ready on %s (dual HTTP & HTTPS enabled; TLS cert: %s)", cfg.ServiceName, cfg.ListenAddr, certInfo)

	return srv.Serve(dualLn)
}
