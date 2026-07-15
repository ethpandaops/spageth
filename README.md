# spageth

<p align="center">
  <img src="docs/spageth.jpg" alt="spageth" width="360">
</p>

<p align="center"><em>geth, but it eats the whole bowl.</em></p>

spageth is ethPandaOps' observability distribution of
[go-ethereum](https://github.com/ethereum/go-ethereum): a normal, fully-synced
geth node carrying a small stack of **independently-toggleable observability
features**. Each feature is off by default (unset flag → stock-geth behaviour),
so one image serves any combination.

| Feature | Enable with | What it does | Transport |
|---|---|---|---|
| **EL node records** | `--xatu.config <path>` | Emits a `NODE_RECORD_EXECUTION` event for every eth handshake (inbound & outbound, success or rejected), capturing the peer population outbound crawlers miss | xatu output client → xatu server |
| **State metrics** | `--vmtrace statesize` | Per-block state write/delete activity (accounts, storage, trie, code — counts, bytes, trie-node depth) as a structured JSON log line | stdout → Vector `sentry-logs` pipeline |

New observability features are added as self-registering live tracers or small
overlay packages — one file, no core-patch growth. See [How it works](#how-it-works).

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
   - `eth/xatuobserver/` + `eth/backend_xatu.go` — the node-records observer. It
     imports xatu's own output client, so batching, retries, gzip and metrics
     behave exactly as they do everywhere else in the ecosystem.
   - `eth/tracers/live/statesize.go` — the state-metrics live tracer (originally
     Wei Han's). It self-registers under geth's existing `--vmtrace statesize`
     and needs **zero** core-patch changes: it compiles against upstream's
     `core/tracing` `OnStateUpdate` hook and emits JSON log lines. Adding a new
     tracer-based feature costs exactly one overlay file.

The base patch stays small and stable precisely because features prefer the
overlay: a feature only touches `base.patch` if it genuinely needs a new core
hook (as node-records does), otherwise it's a self-contained overlay file.

The upstream clone is fetched fresh at build time — this repo contains no
go-ethereum source. `go.mod`/`go.sum` edits are applied by `update-deps.sh`
rather than living in the patch, because dependency diffs conflict on almost
every upstream change.

## Build

```sh
# Clone upstream, apply patch + overlay, wire deps, build the binary.
# --ref takes either a branch (master) or an upstream release tag (v1.17.4).
scripts/spageth-build.sh --ref v1.17.4
# → go-ethereum/build/bin/geth

# Bleeding edge instead:
scripts/spageth-build.sh --ref master
```

Patches are **per upstream ref** — `patches/ethereum/go-ethereum/<ref>/base.patch`.
The core hooks are additive, but the version-sensitive edit (`eth/dropper.go`)
differs between master and each release, so every ref carries its own patch. To
add support for a new geth release, generate its patch (see [Maintenance](#maintenance)).

Or pull a prebuilt image from GitHub Container Registry:

```sh
docker pull ghcr.io/ethpandaops/spageth:latest          # latest geth release + latest overlay
docker pull ghcr.io/ethpandaops/spageth:geth-v1.17.4    # latest overlay on geth v1.17.4
docker pull ghcr.io/ethpandaops/spageth:master          # master canary
```

### Image tag scheme

Tags encode **both** the upstream geth ref and the spageth overlay version, so
you can read "geth vX.Y.Z + spageth overlay vA.B.C" straight off the tag:

| Tag | Meaning | Mutability |
|---|---|---|
| `geth-<GETH_REF>-<SPAGETH_VERSION>` | fully qualified, e.g. `geth-v1.17.4-v0.2.0` | immutable |
| `geth-<GETH_REF>` | latest overlay on that geth ref, e.g. `geth-v1.17.4` | moving |
| `latest` | the latest geth release build | moving |
| `geth-master` / `master` | master canary | moving |

`SPAGETH_VERSION` is the git tag that triggered the release (or a short SHA for
untagged builds); `GETH_REF` is the upstream go-ethereum ref built against.

Or build an image yourself against any ref:

```sh
docker build -f ci/Dockerfile -t spageth --build-arg UPSTREAM_REF=v1.17.4 .
```

## Run

```sh
# node records + state metrics on one node; enable whichever you want
geth --xatu.config /etc/spageth/config.yaml --maxpeers 500 --vmtrace statesize
```

See [`example.config.yaml`](example.config.yaml) for the node-records observer
config. `--vmtrace statesize` needs no config. With neither flag set, spageth
behaves exactly like stock geth.

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

`check-patches.yml` rebuilds spageth against every tracked ref (`master` and the
current release tag) daily. If a 3-way merge is needed it commits the regenerated
patch back automatically; if the patch fails outright the workflow goes red and a
human fixes the clone and runs `scripts/save-patch.sh`. The patch surface is small
and additive, so conflicts are rare and cheap — but this repo still needs an owner
watching that workflow, or it will rot like any fork.

`track-geth-releases.yml` runs daily too and resolves the latest go-ethereum
release. If that release has no `patches/ethereum/go-ethereum/<tag>/` directory
yet, it **fails loudly** — that's the signal to generate the patch for the new
release. Otherwise it builds and publishes the `geth-<tag>` image.

### Generating a patch for a new geth release

```sh
# 1. Full clone at the new tag (full, not --depth 1 — 3-way merge needs blobs).
git clone --branch v1.18.0 https://github.com/ethereum/go-ethereum.git /tmp/geth
# 2. Apply the nearest existing base.patch; resolve rejected hunks. In practice
#    only eth/dropper.go rejects — the churn/drop edit is version-sensitive.
cd /tmp/geth && git apply --reject <spageth>/patches/ethereum/go-ethereum/master/base.patch
#    ...hand-port the dropper.go .rej hunks onto this release's dropper.go...
# 3. Regenerate a clean patch (strips overlay + go.mod, keeps only upstream edits).
CLONE_DIR=/tmp/geth UPSTREAM_REF=v1.18.0 <spageth>/scripts/save-patch.sh
# 4. Confirm it builds, then add the tag to check-patches.yml's matrix and commit.
<spageth>/scripts/spageth-build.sh --ref v1.18.0
```

## Layout

```
patches/ethereum/go-ethereum/<ref>/base.patch   upstream-file edits (shared hooks)
overlay/eth/xatuobserver/                        feature: EL node records (xatu)
overlay/eth/backend_xatu.go                      wires the node-records observer
overlay/eth/tracers/live/statesize.go            feature: state metrics (--vmtrace statesize)
scripts/spageth-build.sh                         clone → apply → deps → build
scripts/apply-patch.sh                           patch + overlay (3-way fallback)
scripts/save-patch.sh                            regenerate base.patch after a fix
scripts/update-deps.sh                           wire go.mod (self-replace + xatu)
ci/Dockerfile                                    multi-stage build
```
