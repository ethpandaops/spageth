# spageth

<p align="center">
  <img src="docs/spageth.jpg" alt="spageth" width="360">
</p>

<p align="center"><em>geth, but it eats the whole bowl.</em></p>

spageth is a thin observability fork of [go-ethereum](https://github.com/ethereum/go-ethereum).
It runs as a normal, fully-synced geth node and exports a `NODE_RECORD_EXECUTION`
event to a [xatu](https://github.com/ethpandaops/xatu) server for **every eth
handshake it performs** — inbound and outbound, successful or rejected. That's
it. No consensus changes, no new RPC surface, no behavioural change when the
observer is switched off.

## Why this exists

xatu's execution-layer coverage under-reports. Discovery finds ~10k unique nodes
over 90 days, but freshness is poor: outbound crawlers can only handshake peers
that have a free slot and are directly dialable, so NATed and slot-full nodes are
invisible, and ~73% of dial attempts are bounced with "too many peers".

A real synced node sees the other side of that: peers dial **it**. spageth is a
well-behaved, snap-advertising, head-serving node that other clients are happy
to connect to and stay connected to — so it observes the population a crawler
structurally can't reach. Reading each peer's `Status` (client, capabilities,
fork id, network id, genesis, head) turns those connections into the same node
records the rest of the xatu pipeline already understands.

We deliberately did **not** hand-roll a devp2p stack (that's what mimicry does,
and maintaining the accept side by hand is painful). We also deliberately did
**not** carry a giant fork. spageth is ~390 lines of patch across 12 upstream
files plus a small overlay package. Everything else is upstream geth, tracked
daily.

## How it works

Two pieces, kept strictly separate so upstream rebases stay cheap:

1. **The patch** (`patches/ethereum/go-ethereum/<ref>/base.patch`) — additive
   edits to 12 upstream files:
   - Capture the peer's raw `Status` packet in `readStatus` **before** validation,
     so we record peers on other networks/forks too.
   - A `peerObserver` hook on the eth handler, fired once per handshake before
     the capacity-based reject.
   - Retain the remote's advertised listen port from the devp2p Hello (otherwise
     lost for inbound peers).
   - Make peer churn and the dial-history window configurable (see flags below).
   - A one-line config field + CLI flag to switch the observer on.

2. **The overlay** (`overlay/`) — new files that never conflict on rebase:
   `eth/xatuobserver/` (the observer: config, event builder, xatu sink lifecycle)
   and `eth/backend_xatu.go` (wires the observer into the backend). The observer
   imports xatu's own output client, so batching, retries, gzip and metrics
   behave exactly as they do everywhere else in the ecosystem.

The upstream clone is fetched fresh at build time — this repo contains no
go-ethereum source. `go.mod`/`go.sum` edits are applied by `update-deps.sh`
rather than living in the patch, because dependency diffs conflict on almost
every upstream change.

## Build

```sh
# Clone upstream, apply patch + overlay, wire deps, build the binary.
scripts/spageth-build.sh --ref master
# → go-ethereum/build/bin/geth
```

Or pull the prebuilt image from GitHub Container Registry:

```sh
docker pull ghcr.io/ethpandaops/spageth:master
```

Or build it yourself:

```sh
docker build -f ci/Dockerfile -t ghcr.io/ethpandaops/spageth --build-arg UPSTREAM_REF=master .
```

## Run

```sh
geth --xatu.config /etc/spageth/config.yaml --maxpeers 500
```

See [`example.config.yaml`](example.config.yaml) for the observer config. With no
`--xatu.config`, spageth behaves exactly like stock geth.

### Configurable everything

All of the harvesting behaviour is exposed as flags — nothing is hardcoded:

| Flag | Default | What it does |
|------|---------|--------------|
| `--xatu.config` | _(off)_ | Path to the observer config; enables event export |
| `--churn.dialhistory` | `35s` | How long a dialed node is excluded from redialing. Crank it up to rotate dial slots through the whole discovered set instead of revisiting the same peers |
| `--churn.drop.min` | `3m` | Minimum interval between capacity-based peer drops |
| `--churn.drop.max` | `7m` | Maximum interval between capacity-based peer drops |
| `--churn.drop.grace` | `10m` | How long a fresh peer is protected from being dropped |

A `0` value on any churn flag means "use the upstream default". Lower the drop
intervals and grace to rotate inbound slots faster and observe more distinct
peers per hour; raise `--churn.dialhistory` to widen outbound coverage.

## Maintenance

`check-patches.yml` rebuilds spageth against upstream `master` every day. If a
3-way merge is needed it commits the regenerated patch back automatically; if the
patch fails outright the workflow goes red and a human fixes the clone and runs
`scripts/save-patch.sh`. The patch surface is small and additive, so conflicts
are rare and cheap — but this repo still needs an owner watching that workflow,
or it will rot like any fork.

## Layout

```
patches/ethereum/go-ethereum/<ref>/base.patch   upstream-file edits
overlay/                                         new files copied into the clone
scripts/spageth-build.sh                         clone → apply → deps → build
scripts/apply-patch.sh                           patch + overlay (3-way fallback)
scripts/save-patch.sh                            regenerate base.patch after a fix
scripts/update-deps.sh                           wire go.mod (self-replace + xatu)
ci/Dockerfile                                    multi-stage build
```
