SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

DC="$SCRIPT_DIR/dc.sh"
"$DC" exec postgres psql -U postgres -d aplobby -c \
  "INSERT INTO room_info (room_id, host, port) VALUES ('$1', '$2', '$3')
   ON CONFLICT (room_id) DO UPDATE SET host = EXCLUDED.host, port = EXCLUDED.port"
