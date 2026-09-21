#!/usr/bin/env bash
set -euo pipefail

# The lane case at the desktop width, captured from the replay build.
README_FILTER='lane/1280'
README_EVIDENCE_DIR='assets/evidence/readme'

# The preview build inherits this, so the frame needs no running services.
export VITE_DATA_SOURCE=replay

source "$(dirname "${BASH_SOURCE[0]}")/lib/preview-server.sh"

SCREENSHOT_BASE_URL="http://localhost:$PREVIEW_PORT" SCREENSHOT_FILTER="$README_FILTER" bun e2e/screenshot.ts

# The filter's section and viewport name the folder and the file prefix the
# capture wrote under screenshots/<hostname>/.
frame_dir="screenshots/localhost/$(dirname "$README_FILTER")"
frame_prefix="$(basename "$README_FILTER")"

mkdir -p "$README_EVIDENCE_DIR"
cp "$frame_dir/$frame_prefix--default.png" "$README_EVIDENCE_DIR/light.png"
cp "$frame_dir/$frame_prefix--dark.png" "$README_EVIDENCE_DIR/dark.png"
