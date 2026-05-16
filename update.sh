#!/usr/bin/env bash
# Build axon from source and install to ~/.local/bin/axon.
# Close any running axon session first — copying over a running binary
# fails with "Text file busy" on Linux.
set -euo pipefail
cd "$(dirname "$0")"
go build -o axon ./cmd/axon
install -m755 axon "$HOME/.local/bin/axon"
rm -f axon
echo "installed: $(command -v axon)"
