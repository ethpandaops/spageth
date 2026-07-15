package xatuobserver

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	xatuproto "github.com/ethpandaops/xatu/pkg/proto/xatu"
)

func TestFirstSeenCacheDedup(t *testing.T) {
	c := newFirstSeenCache(1024)

	hash := common.HexToHash("0xdeadbeef")

	// The first sighting of a hash is new (would emit); every later sighting is
	// a duplicate (would be skipped).
	if c.seen(hash) {
		t.Fatal("first sighting reported as already seen")
	}

	if !c.seen(hash) {
		t.Fatal("second sighting reported as new — dedup failed")
	}

	if !c.seen(hash) {
		t.Fatal("third sighting reported as new — dedup failed")
	}

	// A different hash is independent.
	if c.seen(common.HexToHash("0xfeedface")) {
		t.Fatal("distinct hash reported as already seen")
	}
}

func TestFirstSeenCacheZeroSizeUsesDefault(t *testing.T) {
	c := newFirstSeenCache(0)

	if c.cache.Len() != 0 {
		t.Fatalf("new cache is not empty: len=%d", c.cache.Len())
	}

	// Recording still works with the defaulted capacity.
	if c.seen(common.HexToHash("0x1")) {
		t.Fatal("first sighting reported as already seen")
	}
}

func mempoolTestObserver(t *testing.T, chainID *big.Int) (*Observer, *types.Transaction, common.Address) {
	t.Helper()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	from := crypto.PubkeyToAddress(key.PublicKey)
	to := common.HexToAddress("0x00000000000000000000000000000000000000ff")
	signer := types.LatestSignerForChainID(chainID)

	tx := types.MustSignNewTx(key, signer, &types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     7,
		To:        &to,
		Gas:       21000,
		GasFeeCap: big.NewInt(100),
		GasTipCap: big.NewInt(2),
		Value:     big.NewInt(1000),
		Data:      []byte{0x01, 0x02, 0x03},
	})

	o := &Observer{
		config: &Config{
			Name:           "spageth",
			Version:        "1.0.0",
			Implementation: "spageth",
			Ethereum:       EthereumConfig{Network: NetworkConfig{Name: "mainnet"}},
			Labels:         map[string]string{"region": "syd1"},
			Mempool:        MempoolConfig{Enabled: true},
		},
		signer:    signer,
		networkID: chainID.Uint64(),
	}

	return o, tx, from
}

func TestBuildMempoolEvent(t *testing.T) {
	chainID := big.NewInt(1)
	o, tx, from := mempoolTestObserver(t, chainID)

	event := o.buildMempoolEvent(tx, time.Now())
	if event == nil {
		t.Fatal("buildMempoolEvent returned nil")
	}

	if got := event.GetEvent().GetName(); got != xatuproto.Event_MEMPOOL_TRANSACTION_V2 {
		t.Fatalf("event name = %v, want MEMPOOL_TRANSACTION_V2", got)
	}

	if event.GetEvent().GetId() == "" {
		t.Fatal("event id is empty")
	}

	client := event.GetMeta().GetClient()
	if client.GetModuleName() != xatuproto.ModuleName_EL_MIMICRY {
		t.Errorf("module name = %v, want EL_MIMICRY", client.GetModuleName())
	}

	if got := client.GetEthereum().GetNetwork().GetId(); got != chainID.Uint64() {
		t.Errorf("network id = %d, want %d", got, chainID.Uint64())
	}

	if got := client.GetLabels()["region"]; got != "syd1" {
		t.Errorf("region label = %q, want syd1", got)
	}

	// The payload is the RLP-encoded transaction as a 0x hex string.
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	if got := event.GetMempoolTransactionV2(); got != hexutil.Encode(rawTx) {
		t.Errorf("payload = %q, want %q", got, hexutil.Encode(rawTx))
	}

	add := client.GetAdditionalData().(*xatuproto.ClientMeta_MempoolTransactionV2).MempoolTransactionV2
	if add.GetHash() != tx.Hash().Hex() {
		t.Errorf("additional hash = %q, want %q", add.GetHash(), tx.Hash().Hex())
	}

	if add.GetFrom() != from.Hex() {
		t.Errorf("from = %q, want %q (recovered sender)", add.GetFrom(), from.Hex())
	}

	if add.GetNonce().GetValue() != 7 {
		t.Errorf("nonce = %d, want 7", add.GetNonce().GetValue())
	}

	if add.GetType().GetValue() != uint32(types.DynamicFeeTxType) {
		t.Errorf("type = %d, want %d", add.GetType().GetValue(), types.DynamicFeeTxType)
	}
}
