import argparse
import os
import json
import subprocess

# Copy the Discord information from an existing (remote) ap lobby's api response /api/room/<room_id
# Inserts that information into a matching (local) ap lobby given its room id

SCRIPT_DIR = os.path.dirname(os.path.realpath(__file__))
DC = os.path.join(SCRIPT_DIR, "dc.sh")

def run_postgres_command(command):
  subprocess.run(
    [DC, "exec", "postgres", "psql", "-U", "postgres", "-d", "aplobby", "-c", command],
    check=True
  )

def main():
  parser = argparse.ArgumentParser()

  parser.add_argument("lobby_id", help="Id of the lobby to overwrite discord info for")
  parser.add_argument("json_file", help="Api response of the original lobby")

  args = parser.parse_args()

  with open(args.json_file, "r", encoding="utf-8") as f:
    data = json.load(f)

  yamls = data.get("yamls", [])
  if not yamls:
    print("No yamls found in JSON file.")
    return

  for entry in yamls:
    player_name = entry["player_name"].replace("'", "''")
    discord_id = entry["discord_id"]
    discord_handle = entry["discord_handle"].replace("'", "''")

    upsert_command = (
        f"INSERT INTO discord_users (id, username) "
        f"VALUES ('{discord_id}', '{discord_handle}') "
        f"ON CONFLICT (id) DO UPDATE SET username = '{discord_handle}';"
    )

    update_command = (
        f"UPDATE yamls "
        f"SET owner_id = '{discord_id}' "
        f"WHERE room_id = '{args.lobby_id}' AND player_name = '{player_name}';"
    )

    print(f"Upserting discord user '{discord_handle}' ({discord_id})...")
    run_postgres_command(upsert_command)

    print(f"Updating yaml for player '{player_name}'...")
    run_postgres_command(update_command)

  print(f"Done. Updated {len(yamls)} row(s).")

if __name__ == "__main__":
  main()