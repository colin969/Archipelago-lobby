#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DC="$SCRIPT_DIR/dc.sh"

echo "== Starting Community AP Tools =="
"$DC" up -d community-ap-tools
echo "== Community AP Tools are Ready =="