package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/IrineSistiana/mosdns/v5/coremain"
	"github.com/IrineSistiana/mosdns/v5/pkg/query_context"
	_ "github.com/IrineSistiana/mosdns/v5/plugin"
	"github.com/IrineSistiana/mosdns/v5/plugin/executable/sequence"
	"github.com/miekg/dns"
	"gopkg.in/yaml.v3"
)

func TestMosDNSv5AllModes(t *testing.T) {
	tmpDir := t.TempDir()
	cnFile := filepath.Join(tmpDir, "cn.txt")
	gfwFile := filepath.Join(tmpDir, "gfw.txt")

	if err := os.WriteFile(cnFile, []byte("domain:internal.lan\ndomain:baidu.com\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gfwFile, []byte("domain:google.com\ndomain:youtube.com\n"), 0644); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name string
		yaml string
	}{
		{
			name: "Whitelist Mode",
			yaml: `
plugins:
  - tag: direct_domain_set
    type: domain_set
    args:
      files:
        - ` + cnFile + `

  - tag: forward_local
    type: forward
    args:
      concurrent: 1
      upstreams:
        - addr: "223.5.5.5:53"

  - tag: forward_remote
    type: forward
    args:
      concurrent: 1
      upstreams:
        - addr: "119.29.29.29:53"

  - tag: sync_to_pf
    type: pf_alias
    args:
      socket_path: "/tmp/pf-test.sock"
      table: "GFW_Proxy"
      min_ttl: 60
      max_ttl: 86400
      timeout: "100ms"
      sync: false

  - tag: main_sequence
    type: sequence
    args:
      - matches:
          - qname $direct_domain_set
        exec: $forward_local
      - matches:
          - has_resp
        exec: return
      - exec: $forward_remote
      - exec: $sync_to_pf
`,
		},
		{
			name: "Blacklist Mode",
			yaml: `
plugins:
  - tag: proxy_domain_set
    type: domain_set
    args:
      files:
        - ` + gfwFile + `

  - tag: forward_local
    type: forward
    args:
      concurrent: 1
      upstreams:
        - addr: "223.5.5.5:53"

  - tag: forward_remote
    type: forward
    args:
      concurrent: 1
      upstreams:
        - addr: "119.29.29.29:53"

  - tag: sync_to_pf
    type: pf_alias
    args:
      socket_path: "/tmp/pf-test.sock"
      table: "GFW_Proxy"
      min_ttl: 60
      max_ttl: 86400
      timeout: "100ms"
      sync: false

  - tag: main_sequence
    type: sequence
    args:
      - matches:
          - qname $proxy_domain_set
        exec: $forward_remote
      - matches:
          - qname $proxy_domain_set
        exec: $sync_to_pf
      - matches:
          - has_resp
        exec: return
      - exec: $forward_local
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var rawCfg coremain.Config
			if err := yaml.Unmarshal([]byte(tc.yaml), &rawCfg); err != nil {
				t.Fatalf("[%s] Failed to unmarshal YAML: %v", tc.name, err)
			}

			m, err := coremain.NewMosdns(&rawCfg)
			if err != nil {
				t.Fatalf("[%s] Failed to initialize MosDNS v5.3.4: %v", tc.name, err)
			}
			defer m.CloseWithErr(nil)

			p := m.GetPlugin("main_sequence")
			if p == nil {
				t.Fatalf("[%s] main_sequence plugin not found", tc.name)
			}
			seq, ok := p.(*sequence.Sequence)
			if !ok {
				t.Fatalf("[%s] main_sequence is not *sequence.Sequence", tc.name)
			}

			qMsg := new(dns.Msg)
			qMsg.SetQuestion("internal.lan.", dns.TypeA)
			qCtx := query_context.NewContext(qMsg)
			_ = seq.Exec(context.Background(), qCtx)
		})
	}
}
