# AP Server Stress Tester

Requires Go 1.25+

Run: `GOEXPERIMENT=jsonv2 go run . <server-url> <data-filepath>`

Data can be pulled from `/api/room/<room_id>` and saved to json
Passwords can be pulled from `/api/room/<room_id>/slots_passwords` and saved to json

If a slot name has {NUMBER} then it will fail to auth.

### Arguments

| Argument | Description |
|---|---|
| `server-url` | WebSocket URL of the AP server |
| `data-filepath` | Path to slot data JSON file |

### Options

| Flag | Default | Description |
|---|---|---|
| `--concurrency` | `150` | Max simultaneous WebSocket connections |
| `--check-rate` | `50` | Average checks per second to target across all clients (usually optimistic, will be lower) |
| `--passwords` | None | Path to JSON file with per-slot passwords |
| `--disable-compression` | False | Do not use compression when connecting to the AP server |
| `--reduced-traffic` | False | Ask for reduced traffic for client connections |
