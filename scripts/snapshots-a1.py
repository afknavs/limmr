#!/usr/bin/env python3
"""Offline native audit matrix. Requires Pillow and an already-built unbundled executable."""
import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser()
parser.add_argument("phase", choices=["before", "after"])
parser.add_argument("--binary", type=Path, required=True)
parser.add_argument("--reuse-existing", action="store_true")
parser.add_argument("--only", help="Comma-separated pages to recapture; retain the complete contact sheets")
args = parser.parse_args()
binary = args.binary.resolve()
assert binary.is_file() and ".app/" not in str(binary), "Use the unbundled executable"
catalog = (ROOT / "app/Sources/Limmr/SnapshotRunner.swift").read_text().split("static func run")[0]
pages = re.findall(r'"([a-z0-9-]+)"', catalog)
sizes = ["760x520", "1100x740", "1440x1000"]
output = ROOT / "app/build/a1" / args.phase
output.mkdir(parents=True, exist_ok=True)
homes = ROOT / ".cache/a1"
homes.mkdir(parents=True, exist_ok=True)
jobs = [(page, size, mode) for size in sizes for mode in ["light", "dark"] for page in pages]
if args.only:
    selected = set(args.only.split(","))
    assert selected.issubset(pages)
    jobs = [job for job in jobs if job[0] in selected]

def capture(job):
    page, size, mode = job
    target = output / f"{page}-{mode}-{size}.png"
    width, height = map(int, size.split("x"))
    expected = (width, height + (32 if page == "s2-settings" else 0))
    if args.reuse_existing and target.exists():
        with Image.open(target) as img:
            if img.size == expected: return target.name
    with tempfile.TemporaryDirectory(prefix="home-", dir=homes) as home:
        command = [str(binary), "--snapshot", str(target), "--page", page, "--size", size]
        if mode == "dark": command.append("--dark")
        if page == "people-loading": command.append("--map-loading")
        with (output / f"{page}-{mode}-{size}.log").open("w") as log:
            subprocess.run(command, env={**os.environ, "LIMMR_HOME": home},
                           stdin=subprocess.DEVNULL, stdout=log, stderr=log, check=True, timeout=90)
    with Image.open(target) as img:
        assert img.size == expected, (target, img.size)
        if args.phase == "after" and img.mode == "RGBA":
            assert img.getchannel("A").getextrema() == (255, 255), (target, "unpainted pixels")
    return target.name

with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
    for index, result in enumerate(pool.map(capture, jobs), 1):
        if index % 24 == 0: print(f"{args.phase}: {index}/{len(jobs)}", flush=True)

evidence = ROOT / "docs/images/a1"
evidence.mkdir(parents=True, exist_ok=True)
manifest = {"phase": args.phase, "pages": pages, "sizes": sizes, "appearances": ["light", "dark"],
            "captures": len(pages) * len(sizes) * 2, "sheets": []}
for size in sizes:
    for mode in ["light", "dark"]:
        for offset in range(0, len(pages), 12):
            batch = pages[offset:offset + 12]
            thumb_w, thumb_h = 380, 270
            sheet = Image.new("RGB", (thumb_w * 3, (thumb_h + 26) * ((len(batch) + 2) // 3)), "#e9e9e9")
            draw = ImageDraw.Draw(sheet)
            for index, page in enumerate(batch):
                x, y = (index % 3) * thumb_w, (index // 3) * (thumb_h + 26)
                draw.text((x + 8, y + 5), page, fill="#111111")
                with Image.open(output / f"{page}-{mode}-{size}.png") as img:
                    img.thumbnail((thumb_w - 8, thumb_h))
                    sheet.paste(img.convert("RGB"), (x + 4, y + 25))
            name = f"{args.phase}-{size}-{mode}-{offset // 12 + 1:02}.jpg"
            sheet.save(evidence / name, quality=88)
            manifest["sheets"].append({"file": name, "pages": batch})
(evidence / f"{args.phase}-manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
print(f"{args.phase}: completed {len(jobs)} captures", flush=True)
