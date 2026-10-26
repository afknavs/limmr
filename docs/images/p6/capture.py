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
records = []
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
                with tempfile.TemporaryDirectory(prefix="p6-snapshot-") as home:
                    env = {key: os.environ[key] for key in ["PATH", "HOME", "TMPDIR"] if key in os.environ}
                    env["LIMMR_HOME"] = home
                    with open(os.devnull, "rb") as stdin:
                        result = subprocess.run(command, env=env, stdin=stdin, capture_output=True, timeout=120)
                    if result.returncode:
                        raise RuntimeError(f"Snapshot failed: {name}, exit {result.returncode}")
                records.append({"file": name, "width": width, "height": height,
                                "hover": hover, "lines": lines,
                                "sha256": hashlib.sha256((OUT / name).read_bytes()).hexdigest()})
                print(name, flush=True)
for dark in [False, True]:
    name = f"focus-{'dark' if dark else 'light'}.png"
    with tempfile.TemporaryDirectory(prefix="p6-snapshot-") as home:
        env = {key: os.environ[key] for key in ["PATH", "HOME", "TMPDIR"] if key in os.environ}
        env["LIMMR_HOME"] = home
        with open(os.devnull, "rb") as stdin:
            subprocess.run([str(EXE), "--snapshot", str(OUT / name), "--page", "people-dense",
                            "--size", "1440x1000", "--select-circle", "p4-circle-0"]
                           + (["--dark"] if dark else []), env=env, stdin=stdin,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=120)
    records.append({"file": name, "width": 1440, "height": 1000,
                    "sha256": hashlib.sha256((OUT / name).read_bytes()).hexdigest()})
    print(name, flush=True)
(OUT / "manifest.json").write_text(json.dumps({
    "fixture": "P4DenseMapFixture: 300 invented people, 8 circles, 400 memberships; overview budget 120",
    "executable": "app/.build/release/Limmr",
    "executable_sha256": hashlib.sha256(EXE.read_bytes()).hexdigest(),
    "snapshots": records,
}, indent=2) + "\n")
