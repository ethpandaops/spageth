#!/usr/bin/env bash
# Applies the base patch and copies the overlay files into a fresh upstream
# clone. Idempotent: re-running against an already-patched clone is detected
# and skipped.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/config.sh"

[ -d "$CLONE_DIR" ] || die "clone not found at $CLONE_DIR (run spageth-build.sh)"
[ -f "$PATCH_DIR/base.patch" ] || die "no base.patch for ref '$UPSTREAM_REF' at $PATCH_DIR
  Each upstream ref needs its own patch — the master patch does not apply to release tags.
  Generate one by hand: clone go-ethereum at '$UPSTREAM_REF', apply the nearest existing
  base.patch, resolve any rejected hunks (eth/dropper.go is the version-sensitive one),
  then regenerate with UPSTREAM_REF=$UPSTREAM_REF scripts/save-patch.sh"

cd "$CLONE_DIR"

log "Applying base patch"
if git apply --check "$PATCH_DIR/base.patch" 2>/dev/null; then
  git apply "$PATCH_DIR/base.patch"
  log "Base patch applied cleanly"
elif git apply --reverse --check "$PATCH_DIR/base.patch" 2>/dev/null; then
  log "Base patch already applied, skipping"
else
  log "Clean apply failed, attempting 3-way merge"
  git apply --3way "$PATCH_DIR/base.patch" || {
    log "3-way merge failed; conflict hunks:"
    git apply --reject "$PATCH_DIR/base.patch" 2>&1 || true
    find . -name '*.rej' -print
    die "patch conflicts against upstream '$UPSTREAM_REF' — regenerate with save-patch.sh"
  }
  log "Base patch applied via 3-way merge (patch needs regeneration)"
fi

# Extension patches (01-*.patch, 02-*.patch, ...) applied in order after base.
shopt -s nullglob
for ext in "$PATCH_DIR"/[0-9][0-9]-*.patch; do
  log "Applying extension patch $(basename "$ext")"
  git apply "$ext" || die "extension patch $(basename "$ext") failed"
done
shopt -u nullglob

log "Copying overlay files"
# Copy every overlay file into the clone, preserving directory structure.
(cd "$OVERLAY_DIR" && find . -type f) | while read -r rel; do
  rel="${rel#./}"
  mkdir -p "$CLONE_DIR/$(dirname "$rel")"
  cp "$OVERLAY_DIR/$rel" "$CLONE_DIR/$rel"
done

log "Patch and overlay applied"
