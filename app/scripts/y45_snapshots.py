#!/usr/bin/env python3
"""Y45 invented-data visual matrix; never launches an app bundle."""
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "docs/snapshots/Y45"
PAGES = ["y45-groups", "y45-group", "y45-custom", "y45-initials", "y45-contacts", "y45-lately", "settings-people"]

def main():
    binary = ROOT / "app/.build/debug/Limmr"
    assert binary.is_file() and ".app/" not in str(binary.resolve())
    OUTPUT.mkdir(parents=True, exist_ok=True)
    manifest = []
    with tempfile.TemporaryDirectory(prefix="limmr-y45-snapshots-") as home:
        for page in PAGES:
            for size in ["1280x820", "1000x700"]:
                for theme in ["light", "dark", "contrast"]:
                    path = OUTPUT / f"{page}-{size}-{theme}.png"
                    args = [str(binary), "--snapshot", str(path), "--page", page, "--titled-window", "--size", size]
                    if theme == "dark": args += ["--dark"]
                    if theme == "contrast": args += ["--increase-contrast"]
                    result = subprocess.run(args, env=dict(os.environ, LIMMR_HOME=home), stdin=subprocess.DEVNULL, capture_output=True, timeout=45)
                    if result.returncode: raise SystemExit(f"{path.name}: exit {result.returncode}: {result.stderr.decode()[-800:]}")
                    data = path.read_bytes()
                    assert data[:8] == b"\x89PNG\r\n\x1a\n"
                    actual = struct.unpack(">II", data[16:24])
                    assert actual == tuple(map(int, size.split("x"))), (path, actual)
                    manifest.append(dict(file=path.name, page=page, size=size, theme=theme, exit_code=result.returncode, sha256=hashlib.sha256(data).hexdigest()))
                    print(path.name, flush=True)
    (OUTPUT / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

if __name__ == "__main__": main()
