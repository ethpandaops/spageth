package xatuobserver

import (
	"errors"

	"github.com/ethpandaops/xatu/pkg/output/xatu"
)

// Config controls the xatu observer. It is loaded from the YAML file passed
// via --xatu.config. Everything except the output address has a sensible
// default.
type Config struct {
	// Name is the client name reported to the xatu server. It is rewritten
	// server-side to group/user/name based on the authenticating credential.
	Name string `yaml:"name" default:"spageth"`
	// Version is reported as the client version.
	Version string `yaml:"version"`
	// Implementation is reported as the client implementation.
	Implementation string `yaml:"implementation" default:"spageth"`
	// LoggingLevel controls the observer's own log verbosity.
	LoggingLevel string `yaml:"loggingLevel" default:"info"`
	// Labels are attached to every emitted event's client meta.
	Labels map[string]string `yaml:"labels"`
	// Ethereum describes the network this node is attached to.
	Ethereum EthereumConfig `yaml:"ethereum"`
	// Mempool controls the mempool_transaction first-seen observer.
	Mempool MempoolConfig `yaml:"mempool"`
	// Output is the xatu output client config (address, headers, tls, batching).
	Output xatu.Config `yaml:"output"`
}

// MempoolConfig controls the mempool_transaction first-seen observer. It is
// disabled by default, so node records behave exactly as before unless it is
// switched on.
type MempoolConfig struct {
	// Enabled emits a MEMPOOL_TRANSACTION_V2 event the first time this node
	// receives each transaction over the wire, deduplicated by hash.
	Enabled bool `yaml:"enabled" default:"false"`
	// CacheSize bounds the first-seen dedup cache by number of transaction
	// hashes; older entries are evicted LRU-style to cap memory. Zero uses the
	// default.
	CacheSize int `yaml:"cacheSize" default:"1000000"`
}

// EthereumConfig describes the attached network.
type EthereumConfig struct {
	Network NetworkConfig `yaml:"network"`
}

// NetworkConfig names the network. The numeric id is taken from the peer's
// Status at emit time; Name is the human-friendly label (e.g. "mainnet") used
// as the ClickHouse partition key, so it should be set.
type NetworkConfig struct {
	Name string `yaml:"name" default:"mainnet"`
}

// DefaultConfig returns a Config populated with defaults. Callers unmarshal
// YAML over the top of it.
func DefaultConfig() *Config {
	return &Config{
		Name:           "spageth",
		Implementation: "spageth",
		LoggingLevel:   "info",
		Ethereum: EthereumConfig{
			Network: NetworkConfig{Name: "mainnet"},
		},
	}
}

// Validate checks the required fields are present.
func (c *Config) Validate() error {
	if c.Output.Address == "" {
		return errors.New("output.address is required")
	}

	if c.Ethereum.Network.Name == "" {
		return errors.New("ethereum.network.name is required")
	}

	return nil
}
