package xatuobserver

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/lru"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/p2p"

	xatuproto "github.com/ethpandaops/xatu/pkg/proto/xatu"
)

const (
	defaultMempoolCacheSize = 1_000_000
	blobTxType              = 3
)

// firstSeenCache is a bounded, concurrency-safe set of transaction hashes this
// node has already observed. It uses LRU eviction to cap memory: once it holds
// its capacity in hashes, recording a new one drops the least-recently-seen.
type firstSeenCache struct {
	mu    sync.Mutex
	cache lru.BasicLRU[common.Hash, struct{}]
}

func newFirstSeenCache(size int) *firstSeenCache {
	if size <= 0 {
		size = defaultMempoolCacheSize
	}

	return &firstSeenCache{cache: lru.NewBasicLRU[common.Hash, struct{}](size)}
}

// seen reports whether hash was already recorded, recording it if not. The
// first caller for a given hash gets false; concurrent and later callers get
// true. The check and the insert happen under one lock so exactly one caller
// ever observes a given hash as new.
func (c *firstSeenCache) seen(hash common.Hash) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache.Contains(hash) {
		return true
	}

	c.cache.Add(hash, struct{}{})

	return false
}

// EnableMempool prepares the observer to emit mempool_transaction first-seen
// events. It must be called before ObserveTx is wired into the handler. chainID
// is used to recover transaction senders and to tag events with the network id.
func (o *Observer) EnableMempool(chainID *big.Int) {
	o.firstSeen = newFirstSeenCache(o.config.Mempool.CacheSize)
	o.signer = types.LatestSignerForChainID(chainID)

	if chainID != nil {
		o.networkID = chainID.Uint64()
	}
}

// MempoolEnabled reports whether the mempool_transaction observer is switched on
// in the config.
func (o *Observer) MempoolEnabled() bool {
	return o.config.Mempool.Enabled
}

// ObserveTx is the handler hook fired with every batch of transactions received
// over the wire (broadcast or pooled reply). It emits a single
// MEMPOOL_TRANSACTION_V2 event the first time this node sees each transaction,
// deduplicated by hash. It runs on geth's transaction-handling path, so it does
// only the dedup check under a lock and hands each event to the async sink.
func (o *Observer) ObserveTx(peer *p2p.Peer, txs []*types.Transaction) {
	now := time.Now()

	for _, tx := range txs {
		hash := tx.Hash()
		if o.firstSeen.seen(hash) {
			continue
		}

		o.log.WithFields(logrus.Fields{
			"hash": hash.Hex(),
			"peer": peer.ID().String(),
		}).Trace("First-seen mempool transaction")

		event := o.buildMempoolEvent(tx, now)
		if event == nil {
			continue
		}

		if err := o.sink.HandleNewDecoratedEvent(context.Background(), event); err != nil {
			o.log.WithError(err).Debug("Failed to enqueue mempool transaction event")
		}
	}
}

func (o *Observer) buildMempoolEvent(tx *types.Transaction, firstSeen time.Time) *xatuproto.DecoratedEvent {
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		o.log.WithError(err).WithField("hash", tx.Hash().Hex()).Debug("Failed to marshal mempool transaction")

		return nil
	}

	labels := make(map[string]string, len(o.config.Labels))
	for k, v := range o.config.Labels {
		labels[k] = v
	}

	return &xatuproto.DecoratedEvent{
		Event: &xatuproto.Event{
			Name:     xatuproto.Event_MEMPOOL_TRANSACTION_V2,
			DateTime: timestamppb.New(firstSeen),
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
						Id:   o.networkID,
					},
				},
				AdditionalData: &xatuproto.ClientMeta_MempoolTransactionV2{
					MempoolTransactionV2: o.additionalData(tx),
				},
			},
		},
		Data: &xatuproto.DecoratedEvent_MempoolTransactionV2{
			MempoolTransactionV2: hexutil.Encode(rawTx),
		},
	}
}

func (o *Observer) additionalData(tx *types.Transaction) *xatuproto.ClientMeta_AdditionalMempoolTransactionV2Data {
	var to string
	if tx.To() != nil {
		to = tx.To().Hex()
	}

	var from string
	if o.signer != nil {
		if sender, err := o.signer.Sender(tx); err == nil {
			from = sender.Hex()
		}
	}

	data := &xatuproto.ClientMeta_AdditionalMempoolTransactionV2Data{
		Hash:         tx.Hash().Hex(),
		From:         from,
		To:           to,
		Nonce:        wrapperspb.UInt64(tx.Nonce()),
		GasPrice:     tx.GasPrice().String(),
		Gas:          wrapperspb.UInt64(tx.Gas()),
		Value:        tx.Value().String(),
		Size:         fmt.Sprintf("%d", tx.Size()),
		CallDataSize: fmt.Sprintf("%d", len(tx.Data())),
		Type:         wrapperspb.UInt32(uint32(tx.Type())),
		GasTipCap:    tx.GasTipCap().String(),
		GasFeeCap:    tx.GasFeeCap().String(),
	}

	if tx.Type() == blobTxType {
		hashes := tx.BlobHashes()
		blobHashes := make([]string, len(hashes))

		for i, hash := range hashes {
			blobHashes[i] = hash.Hex()
		}

		data.BlobGas = wrapperspb.UInt64(tx.BlobGas())
		data.BlobGasFeeCap = tx.BlobGasFeeCap().String()
		data.BlobHashes = blobHashes

		if sidecar := tx.BlobTxSidecar(); sidecar != nil {
			var size, emptySize int

			for i := range sidecar.Blobs {
				blob := sidecar.Blobs[i][:]
				size += len(blob)
				emptySize += countConsecutiveEmptyBytes(blob, 4)
			}

			data.BlobSidecarsSize = fmt.Sprintf("%d", size)
			data.BlobSidecarsEmptySize = fmt.Sprintf("%d", emptySize)
		}
	}

	return data
}

// countConsecutiveEmptyBytes counts bytes that belong to a run of zero bytes
// longer than threshold. It mirrors the blob-emptiness metric the xatu sentry
// records so spageth-sourced blob transactions carry the same sidecar fields.
func countConsecutiveEmptyBytes(b []byte, threshold int) int {
	count, run := 0, 0

	for _, v := range b {
		if v == 0 {
			run++

			continue
		}

		if run > threshold {
			count += run
		}

		run = 0
	}

	if run > threshold {
		count += run
	}

	return count
}
