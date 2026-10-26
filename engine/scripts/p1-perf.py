#!/usr/bin/env python3
"""P1 offline fixture, benchmark, 60-second resource counters and optional native UI proof.
Uses only synthetic data and temporary homes. Never starts a live WhatsApp client.
"""
import argparse
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--app', type=Path, help='Built Limmr executable, to measure an offscreen native People view')
parser.add_argument('--output', type=Path, default=ROOT / 'app/build/snapshots/p1')
args = parser.parse_args()
args.output.mkdir(parents=True, exist_ok=True)
with tempfile.TemporaryDirectory(prefix='limmr-p1-') as home:
    fixture = Path(home) / 'fixture'
    env = dict(os.environ, LIMMR_HOME=home, P1_FINAL='1', P1_IDLE='1', P1_EXPORT=str(fixture),
               GOFLAGS='-mod=mod', GOPROXY='off', GOCACHE=str(ROOT / '.cache/go-build'))
    with (args.output / 'engine-perf.txt').open('w') as log:
        subprocess.run(['go', 'test', './internal/perf', '-run', 'TestLargePayloads', '-bench', 'BenchmarkLarge',
                        '-benchtime=3x', '-count=1', '-v'], cwd=ROOT / 'engine', env=env,
                       stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, check=True)
    if args.app:
        app = args.app.resolve()
        for dark in [False, True]:
            mode = 'dark' if dark else 'light'
            command = [str(app), '--snapshot', str(args.output / f'people-{mode}.png'), '--page', 'people-people',
                       '--size', '1100x740', '--perf-fixture', str(fixture)]
            if dark:
                command += ['--dark']
            else:
                command += ['--perf-seconds', '60', '--perf-json', str(args.output / 'app-perf.json')]
            with (args.output / f'app-{mode}.log').open('w') as log:
                subprocess.run(command, env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT,
                               timeout=120, check=True)
print('P1 synthetic evidence:', args.output)
