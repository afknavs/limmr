#!/usr/bin/env python3
"""Render P10's invented-data review matrix with an unbundled executable only."""
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "docs/snapshots/P10"
PAGES = {
    "map": ("people-circles", []),
    "circle-hover": ("people-circle-hover", []),
    "circle-list": ("people-counts", []),
    "people-list": ("people-people", ["--people-list"]),
    "settings-people": ("settings-people", []),
    "person-hover": ("people-map-hover", []),
    "accessible-map": ("people-circles", ["--increase-contrast", "--reduce-motion", "--focus-control", "map"]),
    "circle-keyboard": ("people-circle-hover", ["--focus-control", "circle-label", "--reduce-motion"]),
}


def main():
    binary = Path(sys.argv[1] if len(sys.argv) == 2 else ROOT / "app/.build/debug/Limmr").resolve()
    if not binary.is_file() or ".app/" in str(binary) or binary.suffix == ".app":
        raise SystemExit("Pass the built unbundled Limmr executable.")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    records = []
    with tempfile.TemporaryDirectory(prefix="limmr-p10-") as home:
        env = dict(os.environ, LIMMR_HOME=home)
        for name, (page, extra) in PAGES.items():
            for width, height in [(1100, 740), (1440, 1000)]:
                for theme in ["light", "dark"]:
                    path = OUTPUT / f"{name}-{width}x{height}-{theme}.png"
                    args = [str(binary), "--snapshot", str(path), "--page", page,
                            "--size", f"{width}x{height}", *extra]
                    if theme == "dark":
                        args += ["--dark"]
                    result = subprocess.run(args, env=env, stdin=subprocess.DEVNULL,
                                            capture_output=True, timeout=45)
                    if result.returncode:
                        raise SystemExit(f"{path.name}: exit {result.returncode}: {result.stderr.decode()[-1200:]}")
                    data = path.read_bytes()
                    assert data[:8] == b"\x89PNG\r\n\x1a\n"
                    assert struct.unpack(">II", data[16:24]) == (width, height)
                    records.append(dict(file=path.name, page=page, theme=theme,
                                        width=width, height=height, arguments=extra,
                                        sha256=hashlib.sha256(data).hexdigest()))
                    print(path.name, flush=True)
    (OUTPUT / "manifest.json").write_text(json.dumps(records, indent=2) + "\n")


if __name__ == "__main__":
    main()
