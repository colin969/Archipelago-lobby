#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DC="$SCRIPT_DIR/dc.sh"

echo "== Starting AP WebHost =="
"$DC" up -d ap-webhost
echo "Waiting for AP WebHost..."
until "$DC" exec ap-webhost curl -sf http://127.0.0.1:9888/ > /dev/null 2>&1; do
    sleep 2
done
echo "== AP Webhost is Ready =="

