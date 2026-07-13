// Package xatuobserver exports a NODE_RECORD_EXECUTION event to a xatu server
// for every eth protocol handshake the node performs, inbound or outbound,
// successful or rejected. It reuses xatu's own output client so batching,
// retries, compression and metrics behave exactly as they do elsewhere in the
// xatu ecosystem.
package xatuobserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/ethpandaops/xatu/pkg/output/xatu"
	"github.com/ethpandaops/xatu/pkg/processor"
	"github.com/ethpandaops/xatu/pkg/proto/noderecord"
	xatuproto "github.com/ethpandaops/xatu/pkg/proto/xatu"

	ethproto "github.com/ethereum/go-ethereum/eth/protocols/eth"
)

// Observer builds node record events from eth handshakes and ships them to a
// xatu server. The zero value is not usable; construct one with New.
type Observer struct {
	config *Config
	log    logrus.FieldLogger
	sink   *xatu.Xatu
}

// New constructs an Observer from a config file on disk. It creates but does
// not start the underlying xatu sink; call Start for that.
func New(configPath string) (*Observer, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid xatu observer config: %w", err)
	}

	logger := logrus.New()

	level, err := logrus.ParseLevel(cfg.LoggingLevel)
	if err != nil {
		return nil, fmt.Errorf("invalid logging level %q: %w", cfg.LoggingLevel, err)
	}

	logger.SetLevel(level)

	sink, err := xatu.New(cfg.Name, &cfg.Output, logger, &xatuproto.EventFilterConfig{}, processor.ShippingMethodAsync)
	if err != nil {
		return nil, fmt.Errorf("failed to create xatu output: %w", err)
	}

	if cfg.Output.Headers != nil {
		if auth := cfg.Output.Headers["Authorization"]; auth != "" {
			sink.SetAuthorization(auth)
		}
	}

	return &Observer{
		config: cfg,
		log:    logger.WithField("module", "xatuobserver"),
		sink:   sink,
	}, nil
}

// Start satisfies node.Lifecycle. It starts the underlying sink.
func (o *Observer) Start() error {
	o.log.WithField("server", o.config.Output.Address).Info("Starting xatu observer")

	return o.sink.Start(context.Background())
}

// Stop satisfies node.Lifecycle. It flushes and stops the underlying sink.
func (o *Observer) Stop() error {
	return o.sink.Stop(context.Background())
}

// Observe is the handler wired into the eth protocol handshake path. It is
// invoked once per handshake attempt with the peer and the handshake result
// (nil on success). It never blocks the caller: the event is handed to the
// sink's async batch processor.
func (o *Observer) Observe(peer *ethproto.Peer, handshakeErr error) {
	status := peer.Status()
	if status == nil {
		// No Status was received (the remote hung up before sending one).
		// Without a Status there is no node record worth emitting.
		return
	}

	event := o.buildEvent(newHandshake(peer, status, handshakeErr))

	if err := o.sink.HandleNewDecoratedEvent(context.Background(), event); err != nil {
		o.log.WithError(err).Debug("Failed to enqueue node record event")
	}
}

// handshake is the set of primitives extracted from a peer's handshake that
// the event builder needs. Keeping it a plain struct lets the builder be
// tested without constructing a live p2p peer.
type handshake struct {
	enr             string
	name            string
	capabilities    []string
	inbound         bool
	listenPort      uint64
	protocolVersion uint32
	networkID       uint64
	head            string
	genesis         string
	forkIDHash      string
	forkIDNext      uint64
	err             error
}

func newHandshake(peer *ethproto.Peer, status *ethproto.StatusPacket, handshakeErr error) handshake {
	capabilities := make([]string, 0, len(peer.Caps()))
	for _, cap := range peer.Caps() {
		capabilities = append(capabilities, fmt.Sprintf("%s/%d", cap.Name, cap.Version))
	}

	var enr string
	if node := peer.Node(); node != nil {
		enr = node.String()
	}

	return handshake{
		enr:             enr,
		name:            peer.Fullname(),
		capabilities:    capabilities,
		inbound:         peer.Inbound(),
		listenPort:      peer.RemoteListenPort(),
		protocolVersion: status.ProtocolVersion,
		networkID:       status.NetworkID,
		head:            status.LatestBlockHash.Hex(),
		genesis:         status.Genesis.Hex(),
		forkIDHash:      fmt.Sprintf("0x%x", status.ForkID.Hash),
		forkIDNext:      status.ForkID.Next,
		err:             handshakeErr,
	}
}

func (o *Observer) buildEvent(hs handshake) *xatuproto.DecoratedEvent {
	now := time.Now()

	labels := map[string]string{
		"handshake":   "success",
		"direction":   direction(hs.inbound),
		"listen_port": fmt.Sprintf("%d", hs.listenPort),
	}
	if hs.err != nil {
		labels["handshake"] = "rejected"
		labels["handshake_error"] = hs.err.Error()
	}

	for k, v := range o.config.Labels {
		labels[k] = v
	}

	executionData := &noderecord.Execution{
		Enr:             &wrapperspb.StringValue{Value: hs.enr},
		Timestamp:       timestamppb.New(now),
		Name:            &wrapperspb.StringValue{Value: hs.name},
		Capabilities:    &wrapperspb.StringValue{Value: strings.Join(hs.capabilities, ",")},
		ProtocolVersion: &wrapperspb.StringValue{Value: fmt.Sprintf("%d", hs.protocolVersion)},
		Head:            &wrapperspb.StringValue{Value: hs.head},
		Genesis:         &wrapperspb.StringValue{Value: hs.genesis},
		ForkIdHash:      &wrapperspb.StringValue{Value: hs.forkIDHash},
		ForkIdNext:      &wrapperspb.StringValue{Value: fmt.Sprintf("%d", hs.forkIDNext)},
	}

	return &xatuproto.DecoratedEvent{
		Event: &xatuproto.Event{
			Name:     xatuproto.Event_NODE_RECORD_EXECUTION,
			DateTime: timestamppb.New(now),
			Id:       uuid.New().String(),
		},
		Meta: &xatuproto.Meta{
			Client: &xatuproto.ClientMeta{
				Name:           o.config.Name,
				Version:        o.config.Version,
				Id:             uuid.New().String(),
				Implementation: o.config.Implementation,
				ModuleName:     xatuproto.ModuleName_EL_MIMICRY,
				Labels:         labels,
				Ethereum: &xatuproto.ClientMeta_Ethereum{
					Network: &xatuproto.ClientMeta_Ethereum_Network{
						Name: o.config.Ethereum.Network.Name,
						Id:   hs.networkID,
					},
				},
			},
		},
		Data: &xatuproto.DecoratedEvent_NodeRecordExecution{
			NodeRecordExecution: executionData,
		},
	}
}

func direction(inbound bool) string {
	if inbound {
		return "inbound"
	}

	return "outbound"
}

func loadConfig(path string) (*Config, error) {
	if path == "" {
		return nil, errors.New("xatu observer config path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read xatu observer config: %w", err)
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse xatu observer config: %w", err)
	}

	return cfg, nil
}

var _ interface {
	Start() error
	Stop() error
} = (*Observer)(nil)
