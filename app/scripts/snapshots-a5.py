#!/usr/bin/env python3
"""Capture the A5 synthetic visual matrix with the unbundled snapshot runner."""
import argparse
import os
from pathlib import Path
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PAGES = {
    'home': 'h2-home', 'people-map': 'people-circles', 'people-list': 'people-people',
    'person': 'person-full', 'activity': 'e1-activity', 'effort': 'h2-effort',
    'activity-log': 'e1-log', 'circle': 'circle',
    **{f'settings-{pane}': f'settings-{pane}' for pane in (
        'general', 'sources', 'integrations', 'people', 'briefs', 'hidden', 'remote', 'health', 'advanced')},
}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('stage', choices=['before', 'after'])
    parser.add_argument('--only', nargs='+', help='Recapture named screens after a local refinement.')
    args = parser.parse_args()
    output = ROOT / 'docs/snapshots/A5' / args.stage
    output.mkdir(parents=True, exist_ok=True)
    jobs = [(name, page, width, dark, []) for name, page in PAGES.items()
            for width in (760, 1100, 1600) for dark in (False, True)]
    # Full-height captures also expose content below the usual window viewport.
    for name, page, extra in (
        ('home-empty-triggers', 'h2-empty-triggers', []),
        ('home-quiet', 'u5-home-quiet', []),
        ('people-empty', 'empty-people', []),
        ('people-loading', 'people-loading', ['--map-loading']),
        ('activity-empty', 'empty-activity', []),
        ('activity-log-empty', 'empty-log', []),
        ('integrations-empty', 'empty-integrations', []),
        ('home-loading', 'h2-home', ['--a3-loading']),
    ):
        jobs.extend((name, page, width, dark, extra) for width in (760, 1100, 1600) for dark in (False, True))
    if args.stage == 'after':
        for name, page, flag in (
            ('person-long', 'person-full', '--a5-long-names'),
            ('people-long', 'people-people', '--a5-long-names'),
            ('circle-long', 'circle', '--a5-long-names'),
            ('effort-long', 'h2-effort', '--a5-long-names'),
            ('circle-empty', 'circle', '--a5-empty-sections'),
            ('effort-empty', 'h2-effort', '--a5-empty-sections'),
        ):
            jobs.extend((name, page, width, dark, [flag]) for width in (760, 1100, 1600) for dark in (False, True))
    if args.only:
        jobs = [job for job in jobs if job[0] in args.only]
    def capture(job):
        name, page, width, dark, extra = job
        target = output / f'{name}-{width}-{"dark" if dark else "light"}.png'
        with tempfile.TemporaryDirectory(dir=ROOT / '.cache', prefix='a5-snapshot-') as home:
            result = subprocess.run([
                str(ROOT / 'app/.build/debug/Limmr'), '--snapshot', str(target),
                '--page', page, '--size', f'{width}x1800', *(['--dark'] if dark else []), *extra,
            ], env=dict(os.environ, LIMMR_HOME=home), stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=40)
        if result.returncode:
            raise RuntimeError(f'{target.name}: snapshot failed ({result.returncode})')
        return target.name
    for index, job in enumerate(jobs, 1):
        name = capture(job)
        print(f'{index}/{len(jobs)} {name}', flush=True)

if __name__ == '__main__':
    main()
