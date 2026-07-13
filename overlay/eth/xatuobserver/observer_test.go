package xatuobserver

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	xatuproto "github.com/ethpandaops/xatu/pkg/proto/xatu"
)

func TestLoadConfigAppliesDefaults(t *testing.T) {
	// A minimal config: just the endpoint, auth and network. Batching, retry
	// and keepalive must be filled in from the xatu output client's defaults.
	minimal := `
name: spageth
ethereum:
  network:
    name: sepolia
output:
  address: xatu-grpc.example:443
  tls: true
  headers:
    Authorization: "Basic Zm9vOmJhcg=="
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(minimal), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}

	if cfg.Output.Address != "xatu-grpc.example:443" {
		t.Errorf("address = %q", cfg.Output.Address)
	}

	// These come purely from the struct-tag defaults, not the YAML.
	if cfg.Output.MaxQueueSize != 51200 {
		t.Errorf("MaxQueueSize = %d, want 51200 (default not applied)", cfg.Output.MaxQueueSize)
	}

	if cfg.Output.MaxExportBatchSize != 512 {
		t.Errorf("MaxExportBatchSize = %d, want 512 (default not applied)", cfg.Output.MaxExportBatchSize)
	}

	if cfg.Output.Workers != 1 {
		t.Errorf("Workers = %d, want 1 (default not applied)", cfg.Output.Workers)
	}

	if cfg.Output.Retry.MaxAttempts != 3 {
		t.Errorf("Retry.MaxAttempts = %d, want 3 (default not applied)", cfg.Output.Retry.MaxAttempts)
	}
}

func testObserver(t *testing.T) *Observer {
	t.Helper()

	return &Observer{
		config: &Config{
			Name:           "spageth",
			Version:        "1.0.0",
			Implementation: "spageth",
			Ethereum:       EthereumConfig{Network: NetworkConfig{Name: "mainnet"}},
			Labels:         map[string]string{"region": "syd1"},
		},
	}
}

func sampleHandshake() handshake {
	return handshake{
		enr:             "enr:-test",
		name:            "Geth/v1.17.0-stable/linux-amd64/go1.24.0",
		capabilities:    []string{"eth/69", "snap/1"},
		inbound:         true,
		listenPort:      30303,
		protocolVersion: 69,
		networkID:       1,
		head:            "0xabcd",
		genesis:         "0x1234",
		forkIDHash:      "0xf0afd0e3",
		forkIDNext:      0,
	}
}

func TestBuildEventSuccess(t *testing.T) {
	o := testObserver(t)

	event := o.buildEvent(sampleHandshake())

	if got := event.GetEvent().GetName(); got != xatuproto.Event_NODE_RECORD_EXECUTION {
		t.Fatalf("event name = %v, want NODE_RECORD_EXECUTION", got)
	}

	if event.GetEvent().GetId() == "" {
		t.Fatal("event id is empty")
	}

	client := event.GetMeta().GetClient()
	if client.GetName() != "spageth" {
		t.Errorf("client name = %q, want spageth", client.GetName())
	}

	if client.GetModuleName() != xatuproto.ModuleName_EL_MIMICRY {
		t.Errorf("module name = %v, want EL_MIMICRY", client.GetModuleName())
	}

	if got := client.GetEthereum().GetNetwork().GetName(); got != "mainnet" {
		t.Errorf("network name = %q, want mainnet", got)
	}

	if got := client.GetEthereum().GetNetwork().GetId(); got != 1 {
		t.Errorf("network id = %d, want 1", got)
	}

	if got := client.GetLabels()["handshake"]; got != "success" {
		t.Errorf("handshake label = %q, want success", got)
	}

	if got := client.GetLabels()["direction"]; got != "inbound" {
		t.Errorf("direction label = %q, want inbound", got)
	}

	if got := client.GetLabels()["region"]; got != "syd1" {
		t.Errorf("region label = %q, want syd1 (config labels should be merged)", got)
	}

	data := event.GetNodeRecordExecution()
	if data.GetEnr().GetValue() != "enr:-test" {
		t.Errorf("enr = %q", data.GetEnr().GetValue())
	}

	if data.GetName().GetValue() != "Geth/v1.17.0-stable/linux-amd64/go1.24.0" {
		t.Errorf("name = %q", data.GetName().GetValue())
	}

	if data.GetCapabilities().GetValue() != "eth/69,snap/1" {
		t.Errorf("capabilities = %q, want eth/69,snap/1", data.GetCapabilities().GetValue())
	}

	if data.GetProtocolVersion().GetValue() != "69" {
		t.Errorf("protocol version = %q, want 69", data.GetProtocolVersion().GetValue())
	}

	if data.GetHead().GetValue() != "0xabcd" {
		t.Errorf("head = %q, want 0xabcd", data.GetHead().GetValue())
	}

	if data.GetForkIdHash().GetValue() != "0xf0afd0e3" {
		t.Errorf("fork id hash = %q, want 0xf0afd0e3", data.GetForkIdHash().GetValue())
	}
}

func TestBuildEventRejected(t *testing.T) {
	o := testObserver(t)

	hs := sampleHandshake()
	hs.inbound = false
	hs.err = errors.New("network id mismatch: 137 (!= 1)")

	event := o.buildEvent(hs)

	labels := event.GetMeta().GetClient().GetLabels()
	if labels["handshake"] != "rejected" {
		t.Errorf("handshake label = %q, want rejected", labels["handshake"])
	}

	if labels["direction"] != "outbound" {
		t.Errorf("direction label = %q, want outbound", labels["direction"])
	}

	if labels["handshake_error"] != "network id mismatch: 137 (!= 1)" {
		t.Errorf("handshake_error label = %q", labels["handshake_error"])
	}

	// A rejected peer on another network must still produce a record: capturing
	// wrong-network / wrong-fork peers is the whole point of observing before
	// handshake validation.
	if event.GetNodeRecordExecution() == nil {
		t.Fatal("expected a node record even for a rejected handshake")
	}
}
