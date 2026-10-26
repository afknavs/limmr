#!/usr/bin/env python3
"""Profile/group hidden-chat controls, with invented demo data only."""
import hashlib
import json
import os
from pathlib import Path
import struct
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / "docs/snapshots/Y45"

def main():
    binary = ROOT / "app/.build/debug/Limmr"
    assert binary.is_file() and ".app/" not in str(binary.resolve())
    manifest = []
    with tempfile.TemporaryDirectory(prefix="limmr-y45-hide-") as home:
        for page in ["y45-custom", "y45-group", "y45-hide-person", "y45-hide-group", "y45-hide-no-dm"]:
            for theme in ["light", "dark"]:
                path = OUTPUT / f"hide-{page}-1280x820-{theme}.png"
                args = [str(binary), "--snapshot", str(path), "--page", page, "--titled-window", "--size", "1280x820"]
                if theme == "dark": args.append("--dark")
                result = subprocess.run(args, env=dict(os.environ, LIMMR_HOME=home), stdin=subprocess.DEVNULL, capture_output=True, timeout=45)
                if result.returncode: raise SystemExit(f"{path.name}: exit {result.returncode}: {result.stderr.decode()[-800:]}")
                data = path.read_bytes()
                assert data[:8] == b"\x89PNG\r\n\x1a\n" and struct.unpack(">II", data[16:24]) == (1280, 820)
                manifest.append(dict(file=path.name, page=page, theme=theme, exit_code=result.returncode, sha256=hashlib.sha256(data).hexdigest()))
                print(path.name, flush=True)
    (OUTPUT / "hide-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")

if __name__ == "__main__": main()
