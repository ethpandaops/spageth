#!/usr/bin/env bash
# End-to-end build: clone upstream at the tracked ref, apply patch + overlay,
# wire deps, build the geth binary.
#
# Usage: scripts/spageth-build.sh [--ref <ref>] [--skip-build] [--keep-clone]
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/config.sh"

SKIP_BUILD=0
KEEP_CLONE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --ref) UPSTREAM_REF="$2"; shift 2 ;;
    --skip-build) SKIP_BUILD=1; shift ;;
    --keep-clone) KEEP_CLONE=1; shift ;;
    *) die "unknown argument: $1" ;;
  esac
done

# Recompute derived paths in case --ref changed the ref.
PATCH_DIR="$REPO_ROOT/patches/ethereum/go-ethereum/$UPSTREAM_REF"

if [ "$KEEP_CLONE" -eq 0 ] || [ ! -d "$CLONE_DIR" ]; then
  log "Cloning $UPSTREAM_REPO @ $UPSTREAM_REF"
  rm -rf "$CLONE_DIR"
  git clone --depth 1 --branch "$UPSTREAM_REF" "$UPSTREAM_REPO" "$CLONE_DIR"
else
  log "Reusing existing clone at $CLONE_DIR"
fi

UPSTREAM_REF="$UPSTREAM_REF" PATCH_DIR="$PATCH_DIR" "$REPO_ROOT/scripts/apply-patch.sh"
"$REPO_ROOT/scripts/update-deps.sh"

if [ "$SKIP_BUILD" -eq 1 ]; then
  log "Skipping build (--skip-build)"
  exit 0
fi

cd "$CLONE_DIR"
log "Building geth"
go build -o ./build/bin/geth ./cmd/geth
log "Built $CLONE_DIR/build/bin/geth"
