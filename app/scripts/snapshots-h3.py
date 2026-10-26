#!/usr/bin/env python3
"""Capture invented H3 cases through the unbundled snapshot executable only."""
import argparse
from concurrent.futures import ThreadPoolExecutor
import fcntl
import json
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
OUTPUT = ROOT / 'docs/images/h3'
BINARY = ROOT / 'app/.build/debug/Limmr'

def main(workers=1):
    OUTPUT.mkdir(parents=True, exist_ok=True)
    cases = [(case, size, dark, [])
             for case in ['demo', 'replies', 'history', 'server', 'odd']
             for size in ['1100x740', '1440x1000', '900x1000']
             for dark in [False, True]]
    cases += [('empty', size, dark, []) for size in ['1100x740', '1440x1000'] for dark in [False, True]]
    cases += [('odd', '1440x1000', dark, ['--increase-contrast', '--reduce-motion']) for dark in [False, True]]
    cases += [('odd', '760x1400', dark, []) for dark in [False, True]]
    with ThreadPoolExecutor(max_workers=workers) as renders:
        manifest = list(renders.map(capture, cases))
    (OUTPUT / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')

def capture(case):
    scenario, size, dark, extra = case
    theme = 'dark' if dark else 'light'
    name = f'{scenario}-{size}-{theme}' + ('-accessible' if extra else '') + '.png'
    target = OUTPUT / name
    with tempfile.TemporaryDirectory(dir=ROOT / '.cache', prefix='h3-snapshot-') as home:
        result = subprocess.run([str(BINARY), '--snapshot', str(target), '--page', 'h2-home',
                                 '--h3-scenario', scenario, '--size', size,
                                 *(['--dark'] if dark else []), *extra],
                                stdin=subprocess.DEVNULL, capture_output=True,
                                env=dict(os.environ, LIMMR_HOME=home), timeout=300)
    if result.returncode or not target.exists():
        raise RuntimeError(f'{name}: snapshot failed ({result.returncode})')
    print(name, flush=True)
    return name

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--workers', type=int, choices=[1, 2], default=1)
    args = parser.parse_args()
    # Serialize offscreen captures if another verification command is waiting on SwiftPM.
    with (ROOT / '.cache/h3-snapshots.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        main(args.workers)
