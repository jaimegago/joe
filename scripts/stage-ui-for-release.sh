#!/usr/bin/env bash
# Stages the production web UI into the go:embed tree, using the exact
# source/dest pair the Makefile's build-ui target uses (kept in sync by
# internal/webui/contract_test.go: TestEmbedSourceMatchesViteOutDir). Invoked
# as a goreleaser `before.hooks` entry so every goreleaser invocation —
# `goreleaser release` (the tag-triggered publish), a local `goreleaser
# build`, and the CI snapshot job — stages the real UI before `go build` runs,
# rather than only the Makefile path guaranteeing it.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

EMBED_UI_DIR="internal/webui/dist"

(cd ui && npm ci && npm run build)

rm -rf "$EMBED_UI_DIR/assets" "$EMBED_UI_DIR/index.html" "$EMBED_UI_DIR/vite.svg"
mkdir -p "$EMBED_UI_DIR"
cp -R ui/dist/. "$EMBED_UI_DIR/"
