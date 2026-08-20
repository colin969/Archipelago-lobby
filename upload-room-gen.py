import argparse
import subprocess
import os
import tempfile
import re
import shutil
import uuid
import zipfile

# Uploads an existing zipped gen to an existing ap lobby, ties patches to the correct slots
# Heavily advised for the room to be closed first

SCRIPT_DIR = os.path.dirname(os.path.realpath(__file__))
DC = os.path.join(SCRIPT_DIR, "dc.sh")

def run_postgres_command(command):
  subprocess.run(
    [DC, "exec", "postgres", "psql", "-U", "postgres", "-d", "aplobby", "-c", command],
    check=True
  )

def run_postgres_list_command(command):
  result = subprocess.run(
    [DC, "exec", "-T", "postgres", "psql", "-U", "postgres", "-d", "aplobby", "-t", "-A", "-c", command],
    check=True,
    capture_output=True,
    text=True
  )
  return result.stdout.strip().splitlines()

def main():
  parser = argparse.ArgumentParser()

  parser.add_argument("lobby_id", help="Id of the lobby to upload the gen for")
  parser.add_argument("zip_file", help="Zipped gen file to upload")

  args = parser.parse_args()

  print("Removing outdated generations")

  # Delete old gen if exists
  cmd = "DELETE FROM generations WHERE room_id = '{}'".format(args.lobby_id)
  run_postgres_command(cmd)

  cmd = "UPDATE yamls SET patch = NULL WHERE room_id = '{}'".format(args.lobby_id)
  run_postgres_command(cmd)

  with tempfile.TemporaryDirectory() as tmp_dir:
    with zipfile.ZipFile(args.zip_file, "r") as zf:
      zf.extractall(tmp_dir)

    files = [os.path.join(tmp_dir, f) for f in os.listdir(tmp_dir)]
  
    # Find .archipelago file so we know what the prefix of the rest of the files is
    archipelago_prefix = ""
    for f in files:
      if f.endswith(".archipelago"):
        archipelago_prefix = f[:-len(".archipelago")]
        break

    if archipelago_prefix == "":
      raise Exception("No .archipelago found in zip")

    seed = re.sub(r'^AP[_-]', '', os.path.basename(archipelago_prefix))
    pattern = re.compile(r"AP[-_]" + re.escape(seed) + r"[-_]P(\d+)[-_]")

    # Create new gen folder
    gen_id = str(uuid.uuid4())
    gen_folder = os.path.join(SCRIPT_DIR, "tmp", "gen-output", gen_id)
    os.makedirs(gen_folder, exist_ok=True)

    # Copy gen file to folder
    shutil.copy(args.zip_file, os.path.join(gen_folder, os.path.basename(args.zip_file)))

    # Gather a list of patch filepaths tied to their slot names, copy them into the gen output folder
    patch_files = {}

    for f in files:
        basename = os.path.basename(f)
        match = pattern.search(basename)
        print(f"  {basename!r} -> {'MATCH slot ' + match.group(1) if match else 'no match'}")
        if match:
            slot_num = int(match.group(1))
            patch_files[slot_num] = basename
            shutil.copy(f, os.path.join(gen_folder, basename))

    print("Creating new fake generation")

    # Create a fake generation database entry
    cmd = "INSERT INTO generations (room_id, job_id, status) VALUES ('{}', '{}', '{}')".format(args.lobby_id, gen_id, "Done")
    run_postgres_command(cmd)

    print("Setting {} patch files".format(len(patch_files.items())))

    # Set yaml patch paths
    cmd = "SELECT player_name FROM yamls WHERE room_id = '{}'".format(args.lobby_id)
    player_names = run_postgres_list_command(cmd)
    player_names = sorted(player_names, key=str.lower)

    # Sort player names by alphabetical lowercase, if identical then first always is first

    for slot_num, patch_filename in patch_files.items():
      player_name = player_names[slot_num - 1]
      safe_patch = patch_filename.replace("'", "''")
      safe_player = player_name.replace("'", "''")
      cmd = "UPDATE yamls SET patch = '{}' WHERE player_name = '{}'".format(safe_patch, safe_player)
      run_postgres_command(cmd)

    print("Writing fake generation log")

    # Create an output.log to show that we manually uploaded instead
    output_log_content = "Manually uploaded by server owner"
    output_log_path = os.path.join(gen_folder, "output.log")
    with open(output_log_path, "w") as log_file:
      log_file.write(output_log_content)

    print("Done")

if __name__ == "__main__":
  main()