#!/usr/bin/env bash
# Regenerates base.patch from a modified clone. Run this after resolving a
# patch conflict by hand in the clone. It strips the overlay files and the
# go.mod/go.sum changes (which update-deps.sh reapplies) so the patch only ever
# contains upstream-file edits.
set -euo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/config.sh"

[ -d "$CLONE_DIR" ] || die "clone not found at $CLONE_DIR"

cd "$CLONE_DIR"

log "Restoring dependency files (kept out of the patch)"
git checkout HEAD -- go.mod go.sum 2>/dev/null || true

log "Removing overlay files (kept out of the patch)"
(cd "$OVERLAY_DIR" && find . -type f) | while read -r rel; do
  rel="${rel#./}"
  rm -f "$CLONE_DIR/$rel"
done

log "Writing $PATCH_DIR/base.patch"
mkdir -p "$PATCH_DIR"
git --no-pager diff HEAD > "$PATCH_DIR/base.patch"

log "Regenerated base.patch ($(wc -l < "$PATCH_DIR/base.patch") lines)"
