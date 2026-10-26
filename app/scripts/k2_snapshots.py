#!/usr/bin/env python3
"""K2 demo-only captures and exact PNG comparison against an untouched main build.

Build/copy main's unbundled executable before editing, then:
  python3 app/scripts/k2_snapshots.py before .cache/k2-main/Limmr
  python3 app/scripts/k2_snapshots.py after app/.build/debug/Limmr
  python3 app/scripts/k2_snapshots.py focus app/.build/debug/Limmr
  python3 app/scripts/k2_snapshots.py compare
All processes have closed stdin and a private LIMMR_HOME. Never launches a bundle.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "docs/snapshots/K2"
PAGES = ["home", "people-circles", "activity", "settings-general", "home-dashboard"]
FOCUS = {"see-all": "home", "not-now": "home", "recent": "home", "trigger": "home", "circle": "home", "circle-update": "home",
         "effort": "activity", "map": "people-circles", "map-label": "people-circles", "sidebar": "home",
         "people-circle": "people-circles", "people-person": "people-people"}


def capture(phase, binary):
    binary = Path(binary).resolve()
    if binary.suffix == ".app" or ".app/" in str(binary):
        raise SystemExit("Use an unbundled executable.")
    cache = ROOT / ".cache"
    cache.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir=cache, prefix="k2-snapshot-home-") as home:
        env = dict(os.environ, LIMMR_HOME=home)
        pairs = FOCUS.items() if phase == "focus" else [(None, p) for p in PAGES]
        for focus, page in pairs:
            for dark in (False, True):
                for contrast in ((False,) if focus else (False, True)):
                    name = f"{focus or page}-{'dark' if dark else 'light'}-{'contrast' if contrast else 'normal'}.png"
                    args = [str(binary), "--snapshot", str(OUTPUT / phase / name), "--page", page, "--size", "1440x900"]
                    if dark: args += ["--dark"]
                    if contrast: args += ["--increase-contrast"]
                    if focus: args += ["--focus-control", "map" if focus == "map-label" else focus]
                    if focus == "map-label": args += ["--select-circle", "university"]
                    result = subprocess.run(args, env=env, stdin=subprocess.DEVNULL,
                                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=40)
                    if result.returncode:
                        raise SystemExit(f"{name}: exit {result.returncode}: {result.stderr.decode()[-1000:]}")
                    print(name, flush=True)


def compare():
    records = []
    for page in PAGES:
        for theme in ("light", "dark"):
            name = f"{page}-{theme}-normal.png"
            before = (OUTPUT / "before" / name).read_bytes()
            after = (OUTPUT / "after" / name).read_bytes()
            records.append(dict(file=name, byte_identical=before == after,
                                main_sha256=hashlib.sha256(before).hexdigest(),
                                k2_sha256=hashlib.sha256(after).hexdigest()))
    (OUTPUT / "normal-comparison.json").write_text(json.dumps(records, indent=2) + "\n")
    for item in records:
        print(item["file"], "BYTE-IDENTICAL" if item["byte_identical"] else "DIFFERENT")
    if not all(item["byte_identical"] for item in records):
        raise SystemExit(1)


if __name__ == "__main__":
    if sys.argv[1:] == ["compare"]:
        compare()
    elif len(sys.argv) == 3 and sys.argv[1] in ("before", "after", "focus"):
        capture(*sys.argv[1:])
    else:
        raise SystemExit("Usage: k2_snapshots.py {before|after|focus} <unbundled-binary> | compare")
