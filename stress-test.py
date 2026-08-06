#!/usr/bin/env python3
"""
Archipelago WebSocket Server Stress Tester
Usage: python stress_test.py <server_url> <data_filepath> [--disable-compression] [--concurrency N] [--passwords <filepath>]

Gonna be real, this is pretty jank. It should be 'ok' to make sure nothing totally collapses though.

Data can be pulled from /api/room/<room_id> and saved to json
Passwords can be pulled from /api/room/<room_id>/slots_passwords and saved to json

Some slots may just refuse to connect for various reasons. What you gonna do.
"""

import asyncio
import json
import argparse
import random
import sys
import time
import uuid
from pathlib import Path
from dataclasses import dataclass, field
from typing import Optional
import websockets
from websockets.exceptions import ConnectionClosed, WebSocketException


@dataclass
class PlayerSlot:
    player_name: str
    game: str
    slot_number: int


@dataclass
class ClientStats:
    player_name: str
    game: str
    connected: bool = False
    refused: bool = False
    checks_sent: int = 0
    error: Optional[str] = None


async def run_game_client(
    server_url: str,
    slot: PlayerSlot,
    stats: ClientStats,
    semaphore: asyncio.Semaphore,
    compression,
    connect_limiter,
    password: str = "",
) -> None:
    async with semaphore:
        try:
            await _client_session(server_url, slot, stats, compression, connect_limiter, password)
        except Exception as e:
            stats.error = str(e)


async def _client_session(
    server_url: str,
    slot: PlayerSlot,
    stats: ClientStats,
    compression,
    connect_limiter,
    password: str
) -> None:
    client_uuid = str(uuid.uuid4())
    version = {"major": 0, "minor": 6, "build": 0, "class": "Version"}

    # Wait our turn, staggered
    await connect_limiter.acquire()

    try:
        async with websockets.connect(server_url, max_size=10 * 1024 * 1024, compression=compression) as ws:
            # Receive RoomInfo
            raw = await asyncio.wait_for(ws.recv(), timeout=15)
            packets = json.loads(raw)
            room_info = next((p for p in packets if p.get("cmd") == "RoomInfo"), None)
            if room_info is None:
                stats.error = "No RoomInfo received"
                return

            # Send Connect
            connect_packet = json.dumps([{
                "cmd": "Connect",
                "password": password,
                "game": slot.game,
                "name": slot.player_name,
                "uuid": client_uuid,
                "version": version,
                "items_handling": 0b111,
                "tags": ["AP"],
                "slot_data": False,
            }])
            await ws.send(connect_packet)

            # Receive Connected or ConnectionRefused
            missing_locations = []
            checked_locations = []
            while True:
                raw = await asyncio.wait_for(ws.recv(), timeout=15)
                packets = json.loads(raw)

                refused = next((p for p in packets if p.get("cmd") == "ConnectionRefused"), None)
                if refused:
                    errors = refused.get("errors", [])
                    stats.refused = True
                    stats.error = f"ConnectionRefused: {errors}"
                    return

                connected = next((p for p in packets if p.get("cmd") == "Connected"), None)
                if connected:
                    missing_locations = list(connected.get("missing_locations", []))
                    checked_locations = set(connected.get("checked_locations", []))
                    stats.connected = True
                    break

            total_locations = len(missing_locations) + len(checked_locations)

            if not missing_locations:
                # Nothing to check, exit
                return

            # Send location checks at roughly 0.75 second intervals
            async def send_checks() -> None:
                for loc_id in missing_locations:
                    check_packet = json.dumps([{
                        "cmd": "LocationChecks",
                        "locations": [loc_id],
                    }])
                    await ws.send(check_packet)
                    stats.checks_sent += 1
                    await asyncio.sleep(random.uniform(2, 3))

            # Drain messages otherwise we'll fail our own timeout never receiving the pong
            async def drain_recv() -> None:
                while True:
                    try:
                        raw = await asyncio.wait_for(ws.recv(), timeout=30)
                        packets = json.loads(raw)
                        room_update = next((p for p in packets if p.get("cmd") == "RoomUpdate"), None)
                        if room_update is not None:
                            new_checked = room_update.get("checked_locations", [])
                            checked_locations.update(new_checked)
                            if len(checked_locations) >= total_locations:
                                break
                    except asyncio.TimeoutError:
                        if len(checked_locations) >= total_locations:
                            break
                        stats.error = "Timeout waiting for server response"
                        break

            await asyncio.gather(send_checks(), drain_recv())

            # Send proper close handshake
            try:
                await ws.close()
            except ConnectionClosed:
                pass 

    except asyncio.TimeoutError:
        stats.error = "Timeout waiting for server response"
    except ConnectionClosed as e:
        if stats.connected:
            pass
        else:
            stats.error = f"Connection closed early: {e}"
    except WebSocketException as e:
        stats.error = f"WebSocket error: {e}"
    except OSError as e:
        stats.error = f"Network error: {e}"

# Load /api/room/<room_id> json output for slot info
def load_slots(filepath: str) -> list[PlayerSlot]:
    data = json.loads(Path(filepath).read_text(encoding="utf-8"))
    yamls = data.get("yamls", [])
    slots = []
    for entry in yamls:
        player_name = entry.get("player_name")
        game = entry.get("game")
        slot_number = entry.get("slot_number", 0)
        if player_name and game:
            slots.append(PlayerSlot(
                player_name=player_name,
                game=game,
                slot_number=slot_number,
            ))
    return slots


def print_summary(all_stats: list[ClientStats], elapsed: float) -> None:
    total = len(all_stats)
    connected = sum(1 for s in all_stats if s.connected)
    refused = sum(1 for s in all_stats if s.refused)
    errored = sum(1 for s in all_stats if s.error and not s.refused)
    total_checks = sum(s.checks_sent for s in all_stats)

    minutes, seconds = divmod(elapsed, 60)

    print("\n" + "=" * 60)
    print(f"STRESS TEST SUMMARY")
    print("=" * 60)
    print(f"  Total clients:    {total}")
    print(f"  Connected:        {connected}")
    print(f"  Refused:          {refused}")
    print(f"  Errored:          {errored}")
    print(f"  Total checks sent:{total_checks}")
    print(f"  Time taken:       {int(minutes)}m {seconds:.1f}s")

    print("=" * 60)

    if errored > 0:
        print("\nErrors:")
        for s in all_stats:
            if s.error and not s.refused:
                print(f"  [{s.player_name} / {s.game}] {s.error}")

    if refused > 0:
        print("\nRefused connections:")
        for s in all_stats:
            if s.refused:
                print(f"  [{s.player_name} / {s.game}] {s.error}")

def load_passwords(filepath: str) -> dict[str, str]:
    data = json.loads(Path(filepath).read_text(encoding="utf-8"))
    return {entry["player_name"]: entry.get("password", "") for entry in data}

async def main() -> None:
    parser = argparse.ArgumentParser(description="Archipelago WebSocket stress tester")
    parser.add_argument("server_url", help="WebSocket server URL, e.g. ws://localhost:38281")
    parser.add_argument("data_filepath", help="Path to the JSON file with player/game data")
    parser.add_argument(
        "--concurrency", type=int, default=150,
        help="Max simultaneous WebSocket connections (default: 150)"
    )
    parser.add_argument(
      "--passwords", type=str, default=None,
      help="Path to JSON file with per-slot passwords"
    )
    parser.add_argument(
      "--disable-compression", action="store_true", default=False,
      help="Do not use compression when connecting to the AP server"
    )
    args = parser.parse_args()

    compression = None if args.disable_compression else "deflate"

    print(f"Loading slots from: {args.data_filepath}")
    slots = load_slots(args.data_filepath)

    if not slots:
        print("No valid player/game entries found in file.")
        sys.exit(1)

    passwords: dict[str, str] = {}
    if args.passwords:
        print(f"Loading passwords from: {args.passwords}")
        passwords = load_passwords(args.passwords)

    print(f"Loaded {len(slots)} player slots")
    print(f"Connecting to: {args.server_url}")
    print(f"Concurrency limit: {args.concurrency}")
    print(f"Starting stress test...\n")
    start_time = time.monotonic()

    semaphore = asyncio.Semaphore(args.concurrency)
    all_stats: list[ClientStats] = []
    tasks = []

    connect_limiter = asyncio.Semaphore(0)

    async def release_tokens() -> None:
        """Release one connection token every ~50ms (20 connects/sec)"""
        for _ in range(len(slots)):
            connect_limiter.release()
            await asyncio.sleep(0.05)

    asyncio.create_task(release_tokens())

    for slot in slots:
        stats = ClientStats(player_name=slot.player_name, game=slot.game)
        all_stats.append(stats)
        password = passwords.get(slot.player_name, "")
        task = asyncio.create_task(
            run_game_client(args.server_url, slot, stats, semaphore, compression, connect_limiter, password)
        )
        tasks.append(task)

    async def report_progress() -> None:
        prev_checks = 0
        while True:
            await asyncio.sleep(5)
            done = sum(1 for t in tasks if t.done())
            connected = sum(1 for s in all_stats if s.connected)
            checks = sum(s.checks_sent for s in all_stats)
            rate = (checks - prev_checks) / 5
            prev_checks = checks
            timestamp = time.strftime("%H:%M:%S")
            print(f"  [{timestamp}] Progress: {done}/{len(tasks)} done | "
                  f"{connected} connected | {checks} checks sent | {rate:.1f} checks/s")
            if done == len(tasks):
                break

    reporter = asyncio.create_task(report_progress())
    await asyncio.gather(*tasks, return_exceptions=True)
    reporter.cancel()

    elapsed = time.monotonic() - start_time
    print_summary(all_stats, elapsed)


if __name__ == "__main__":
    asyncio.run(main())
