#!/usr/bin/env python3
"""Capture only the unbundled release executable, using invented dense map data."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[3]
OUT = Path(__file__).resolve().parent
EXE = ROOT / "app/.build/release/Limmr"
OUT.mkdir(parents=True, exist_ok=True)
(ROOT / ".cache/p7-homes").mkdir(parents=True, exist_ok=True)
records = []

def record(name, width, height, **state):
    data = (OUT / name).read_bytes()
    if data[:8] != b"\x89PNG\r\n\x1a\n":
        raise RuntimeError(f"Invalid PNG: {name}")
    actual = (int.from_bytes(data[16:20], "big"), int.from_bytes(data[20:24], "big"))
    if actual != (width, height):
        raise RuntimeError(f"Wrong dimensions: {name}: {actual}")
    records.append({"file": name, "width": width, "height": height, **state,
                    "sha256": hashlib.sha256(data).hexdigest()})

for width, height in [(1100, 740), (1440, 1000)]:
    for dark in [False, True]:
        for hover in [False, True]:
            for lines in [False, True]:
                name = f"{'hover' if hover else 'overview'}-{width}-{'dark' if dark else 'light'}-lines-{'on' if lines else 'off'}.png"
                command = [str(EXE), "--snapshot", str(OUT / name), "--page",
                           "people-dense-hover" if hover else "people-dense",
                           "--size", f"{width}x{height}"]
                if dark:
                    command += ["--dark"]
                if lines:
                    command += ["--drift-lines"]
                with tempfile.TemporaryDirectory(prefix="p7-snapshot-", dir=ROOT / ".cache/p7-homes") as home:
                    env = {key: os.environ[key] for key in ["PATH", "HOME", "TMPDIR"] if key in os.environ}
                    env["LIMMR_HOME"] = home
                    with open(os.devnull, "rb") as stdin:
                        result = subprocess.run(command, env=env, stdin=stdin, capture_output=True, timeout=120)
                    if result.returncode:
                        raise RuntimeError(f"Snapshot failed: {name}, exit {result.returncode}")
                record(name, width, height, hover=hover, lines=lines)
                print(name, flush=True)
for dark in [False, True]:
    name = f"focus-{'dark' if dark else 'light'}.png"
    with tempfile.TemporaryDirectory(prefix="p7-snapshot-", dir=ROOT / ".cache/p7-homes") as home:
        env = {key: os.environ[key] for key in ["PATH", "HOME", "TMPDIR"] if key in os.environ}
        env["LIMMR_HOME"] = home
        with open(os.devnull, "rb") as stdin:
            subprocess.run([str(EXE), "--snapshot", str(OUT / name), "--page", "people-dense",
                            "--size", "1440x1000", "--select-circle", "p4-circle-0"]
                           + (["--dark"] if dark else []), env=env, stdin=stdin,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=120)
    record(name, 1440, 1000)
    print(name, flush=True)
(OUT / "manifest.json").write_text(json.dumps({
    "fixture": "P4DenseMapFixture: 300 invented people, 8 circles, 400 memberships; overview budget 120",
    "executable": "app/.build/release/Limmr",
    "executable_sha256": hashlib.sha256(EXE.read_bytes()).hexdigest(),
    "snapshots": records,
}, indent=2) + "\n")
