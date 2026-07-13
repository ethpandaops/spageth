#!/usr/bin/env bash
# Shared configuration for the spageth build scripts.
set -euo pipefail

# Upstream repository and the ref we track.
UPSTREAM_REPO="${UPSTREAM_REPO:-https://github.com/ethereum/go-ethereum.git}"
UPSTREAM_REF="${UPSTREAM_REF:-master}"

# The xatu module version to pull in for the observer overlay.
XATU_VERSION="${XATU_VERSION:-latest}"

# Repo root (one level up from scripts/).
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Where the upstream clone lands. Gitignored.
CLONE_DIR="${CLONE_DIR:-$REPO_ROOT/go-ethereum}"

# Patch and overlay locations for the current ref.
PATCH_DIR="$REPO_ROOT/patches/ethereum/go-ethereum/$UPSTREAM_REF"
OVERLAY_DIR="$REPO_ROOT/overlay"

log()  { echo "[spageth] $*"; }
die()  { echo "[spageth] ERROR: $*" >&2; exit 1; }
