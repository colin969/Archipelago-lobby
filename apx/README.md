# APX  (Archipelago Expanded)

Acts as a proxy server sitting between the AP Multiserver and clients.

This is designed to work under a Linux host, mileage may vary on Windows / Mac.

# Capabilities

- Per-slot passwords, loaded from an optionally linked Ionium Lobby
- Offloads burden of DataPackages and Bounces from the AP Multiserver
- Secondary websocket for reduced traffic
  - Limits PrintJSON messages which aren't relevant to the connected slot
- Bounce management
  - Exclude specific slots from sending bounce packets with certain tags (e.g DeathLink)
- Deathlink Management
  - Monitor how many deaths have been sent by each slot
  - Global probability value for how likely a deathlink is sent to other players (default: 1)

## HTTP API

All endpoints require `X-API-Key` header.


### `POST /api/refresh_passwords`
Reload slot passwords from Ionium Lobby. Returns `403` if lobby not enabled.

### `GET /api/deathlinks`
Map of slot → death count.

Response:
```json
{ "Player": 3, "Player2": 0 }
```

### `GET /api/bounce_exclusions`
Map of slot → excluded tags.

Response:
```json
{ 
  "Player": ["DeathLink" "TrapLink"], 
  "Player2": ["DeathLink"] 
}
```

### `POST /api/bounce_exclusions/{slot}/{tag}`
Exclude slot from sending bounces with tag.

### `DELETE /api/bounce_exclusions/{slot}/{tag}`
Remove exclusion.

### `GET /api/deathlink_probability`
Response:
```json
{ "probability": 1 }
```

### `POST /api/deathlink_probability`
Set probability (0–1, default 1).

Request:
```json
{ "probability": 0.5 }
```


# Building

`go build .`

# Testing

`go test .`

# Important Notes

Without modifying your file descriptor limits, APX may crash at extreme client numbers

To increase fd limits, use either
`ulimit -n 1048576` (resets on restart)
or in a systemd unit
```
[Service]
LimitNOFILE=1048576
```