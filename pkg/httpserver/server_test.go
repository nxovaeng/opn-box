package httpserver

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestDualListener(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		isTLS := r.TLS != nil
		fmt.Fprintf(w, "pong,tls=%v", isTLS)
	})

	cfg := Config{
		ListenAddr:  "127.0.0.1:0",
		ServiceName: "test-server",
	}

	ln, err := netListenPort()
	if err != nil {
		t.Fatalf("failed to get free port: %v", err)
	}
	cfg.ListenAddr = ln

	go func() {
		_ = ListenAndServeDual(cfg, mux)
	}()

	time.Sleep(100 * time.Millisecond)

	// Test 1: Plain HTTP
	respHTTP, err := http.Get("http://" + cfg.ListenAddr + "/ping")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer respHTTP.Body.Close()
	bHTTP, _ := io.ReadAll(respHTTP.Body)
	if string(bHTTP) != "pong,tls=false" {
		t.Fatalf("Expected 'pong,tls=false', got: %s", string(bHTTP))
	}

	// Test 2: HTTPS (with fallback self-signed cert)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	respHTTPS, err := client.Get("https://" + cfg.ListenAddr + "/ping")
	if err != nil {
		t.Fatalf("HTTPS GET failed: %v", err)
	}
	defer respHTTPS.Body.Close()
	bHTTPS, _ := io.ReadAll(respHTTPS.Body)
	if string(bHTTPS) != "pong,tls=true" {
		t.Fatalf("Expected 'pong,tls=true', got: %s", string(bHTTPS))
	}
}

func netListenPort() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr, nil
}
