#!/usr/bin/env python3
"""Setup guide's invented-data native captures and print verification. Offline only."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
from PIL import Image

ROOT = Path(__file__).resolve().parents[3]
OUT = ROOT / 'docs/setup'
CACHE = ROOT / 'docs/pdf/.cache/setup'
PDF = ROOT / 'docs/pdf/Limmr-Setup-Guide.pdf'
# Logical point crops, applied to native 3x pixels; never rescale screenshots.
SCENES = {
 'link-qr': ('setup-qr', (420, 125, 1020, 800), []),
 'ai-picker': ('connect-picker', (450, 215, 990, 685), []),
 'local-client': ('connect-codex', (430, 140, 1010, 760), []),
 'remote-ready': ('remote', (400, 150, 1040, 370), ['--remote-ready']),
 'pair-approval': ('remote-approval', (430, 140, 1010, 760), []),
 'home': ('home', (195, 20, 1095, 680), []),
}
def sha(path): return hashlib.sha256(path.read_bytes()).hexdigest()

def capture(binary=None):
    spec = importlib.util.spec_from_file_location('native_capture', Path(__file__).with_name('capture.py'))
    module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
    CACHE.mkdir(parents=True, exist_ok=True); OUT.mkdir(parents=True, exist_ok=True)
    binary = binary.resolve() if binary else module.retina_binary(3).resolve()
    assert binary.is_relative_to(ROOT) and not any(p.endswith('.app') for p in binary.parts)
    # Sample resource lookup uses the disposable package's source path. Supply the
    # same instructions and guide as a normal checkout, without installing either.
    package = ROOT / 'docs/pdf/.cache/capture/app'
    shutil.copytree(ROOT / 'integrations', package.parent / 'integrations', dirs_exist_ok=True)
    shutil.copytree(ROOT / 'app/Resources', package / 'Resources', dirs_exist_ok=True)
    records = []
    scenes = dict(SCENES)
    scenes['copy-prompt-dark'] = ('connect-codex', (430, 140, 1010, 760), ['--dark'])
    scenes['hosted-prompt'] = ('connect-chatgpt', (430, 140, 1010, 760), [])
    for name, (page, crop, flags) in scenes.items():
        full = CACHE / (name + '-full.png')
        with tempfile.TemporaryDirectory(prefix='home-', dir=CACHE) as home:
            subprocess.run([str(binary), '--snapshot', str(full), '--page', page, '--size', '1440x900', *flags],
                env=dict(os.environ, LIMMR_HOME=home), stdin=subprocess.DEVNULL,
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=45)
        with Image.open(full) as source:
            assert source.size == (4320, 2700), 'Native 3x allocation required'
            result = source.crop(tuple(v * 3 for v in crop))
            dest = OUT / (name + '.png'); result.save(dest)
        records.append(dict(file=dest.name, page=page, flags=flags, crop_points=crop,
            scale=3, pixels=list(result.size), sha256=sha(dest), resampled=False))
        print(name + ': native 3x demo capture', flush=True)
    (OUT / 'manifest.json').write_text(json.dumps(dict(data='invented DemoBackend only',
        renderer='unbundled --snapshot, native 3x bitmap allocation via existing PDF capture pipeline',
        binary_sha256=sha(binary), captures=records), indent=2) + '\n')

def verify():
    import sys
    sys.path.insert(0, str(Path(__file__).parent))
    from verify import Document
    manifest = json.loads((OUT / 'manifest.json').read_text())
    assert manifest['data'] == 'invented DemoBackend only'
    for row in manifest['captures']:
        path = OUT / row['file']; assert sha(path) == row['sha256']
        with Image.open(path) as im: assert list(im.size) == row['pixels']
        assert row['scale'] == 3 and not row['resampled']
    doc = Document(ROOT / 'docs/pdf/src/setup-guide.html'); doc.feed(doc.path.read_text())
    assert doc.pages == 11 and doc.images == doc.captions == 6
    assert doc.page_images == [0,0,0,1,1,1,1,1,1,0,0]
    print('PASS: 11 setup pages, 6 captioned native images, local resources and print resolution')

def verify_pdf():
    verify()
    info = subprocess.check_output(['pdfinfo', '-f', '1', '-l', '11', '-box', str(PDF)], text=True)
    import re
    assert re.search(r'Pages:\s+11\b', info), 'Expected eleven PDF pages'
    sizes = re.findall(r'Page\s+\d+ size:\s+([\d.]+) x ([\d.]+)', info)
    assert len(sizes) == 11 and all(abs(float(w)-595)<2 and abs(float(h)-842)<2 for w,h in sizes)
    text = subprocess.check_output(['pdftotext', str(PDF), '-'], text=True)
    pages = [p for p in text.split('\f') if p.strip()]
    assert len(pages) == 11 and all('Limmr' in p for p in pages)
    listing = subprocess.check_output(['pdfimages', '-list', str(PDF)], text=True)
    images = [line.split() for line in listing.splitlines()[2:] if line.strip()]
    images = [row for row in images if row[2] == 'image']
    assert [int(row[0]) for row in images] == [4,5,6,7,8,9]
    assert all(min(float(row[12]),float(row[13]))>=220 for row in images)
    dest = ROOT / 'app/Resources/Limmr-Setup-Guide.pdf'
    shutil.copyfile(PDF, dest)
    assert sha(PDF) == sha(dest)
    print('PASS: 11 A4 PDF pages; embedded rasters >=220 ppi; app resource matches')

if __name__ == '__main__':
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--capture', action='store_true');p.add_argument('--pdf', action='store_true')
    p.add_argument('--binary', type=Path, help='Reuse this worktree’s native 3x unbundled capture binary')
    args=p.parse_args()
    if args.capture: capture(args.binary)
    elif args.pdf: verify_pdf()
    else: verify()
