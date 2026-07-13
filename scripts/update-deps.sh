#!/usr/bin/env bash
# Wires the module dependencies the overlay needs into a prepared upstream
# clone. Done here rather than in the patch because go.mod/go.sum diffs conflict
# on almost every upstream change.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/config.sh"

cd "$CLONE_DIR"

log "Wiring module dependencies (xatu=$XATU_VERSION)"

# The observer imports the xatu module, whose own go.mod requires
# go-ethereum. A self-replace makes that requirement resolve to this fork
# instead of a released go-ethereum. Verified safe: no xatu package in the
# import closure of pkg/output/xatu compiles go-ethereum code.
go mod edit -replace github.com/ethereum/go-ethereum=./

# xatu needs a newer Go than upstream geth pins.
go mod edit -go=1.26.2 -toolchain=go1.26.5

# XATU_REPLACE points the xatu module at a local checkout instead of a released
# version. Used for local development and CI against an unreleased xatu.
if [ -n "${XATU_REPLACE:-}" ]; then
  log "Replacing xatu module with local checkout at $XATU_REPLACE"
  go mod edit -replace "github.com/ethpandaops/xatu=$XATU_REPLACE"
  # A require entry is still needed for the replace to take effect.
  go mod edit -require "github.com/ethpandaops/xatu@v0.0.0-00010101000000-000000000000"
else
  # Pull the xatu module. Add packages explicitly so go records them even
  # though they are only referenced from overlay files.
  go get "github.com/ethpandaops/xatu@${XATU_VERSION}"
  go get "github.com/ethpandaops/xatu/pkg/output/xatu@${XATU_VERSION}"
fi

go mod tidy

log "Module dependencies wired"
