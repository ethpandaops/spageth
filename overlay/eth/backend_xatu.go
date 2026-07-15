package eth

import (
	"github.com/ethereum/go-ethereum/eth/xatuobserver"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
)

// initXatuObserver wires the xatu observer into the eth handshake path when a
// config file was supplied via --xatu.config. It is a no-op otherwise, so an
// unconfigured node behaves exactly like upstream geth.
func (s *Ethereum) initXatuObserver(stack *node.Node) error {
	if s.config.XatuConfig == "" {
		return nil
	}

	observer, err := xatuobserver.New(s.config.XatuConfig)
	if err != nil {
		return err
	}

	s.handler.peerObserver = observer.Observe

	if observer.MempoolEnabled() {
		observer.EnableMempool(s.blockchain.Config().ChainID)
		s.handler.txObserver = observer.ObserveTx

		log.Info("Xatu mempool_transaction observer enabled")
	}

	stack.RegisterLifecycle(observer)

	log.Info("Xatu observer enabled", "config", s.config.XatuConfig)

	return nil
}
