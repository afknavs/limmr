#!/usr/bin/env python3
"""Capture K1 with an unbundled executable and invented demo data only.

The flag sets a high-contrast NSAppearance and the app's snapshot override for
read-only SwiftUI accessibility environments. It never changes macOS preferences.
"""
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
APP = ROOT / "app"
OUTPUT = ROOT / "docs/snapshots/K1"
PAGES = [
    ("home", False), ("home-dashboard", False),
    ("people-circles", False), ("people-people", False),
    ("activity", False), ("circle", False), ("person-full", False),
    ("h2-effort", False), ("settings-general", False),
    ("home", True), ("people-circles", True), ("activity", True),
]


def main():
    binary = next((p for p in [
        APP / ".build/debug/Limmr", APP / ".build/out/Products/Debug/Limmr"
    ] if p.is_file()), None)
    if binary is None:
        raise SystemExit("Run python3 app/scripts/test_serial.py first.")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="k1-snapshots-", dir=ROOT / ".cache") as home:
        env = dict(os.environ, LIMMR_HOME=home)
        for page, dark in PAGES:
            name = page + ("-dark" if dark else "-light") + ".png"
            command = [str(binary), "--snapshot", str(OUTPUT / name), "--page", page,
                       "--size", "1440x1000", "--increase-contrast", "--reduce-motion"]
            if dark:
                command.append("--dark")
            result = subprocess.run(command, env=env, stdin=subprocess.DEVNULL,
                                    stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                    timeout=60, text=True)
            if result.returncode:
                # Demo output only; no real backend or credentials are loaded.
                raise SystemExit(f"Snapshot failed for {page}:\n{result.stdout[-3000:]}")
            if not (OUTPUT / name).is_file():
                raise SystemExit(f"Snapshot did not produce {name}")
            print(name, flush=True)


if __name__ == "__main__":
    main()
