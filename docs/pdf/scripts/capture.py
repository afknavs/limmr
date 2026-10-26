#!/usr/bin/env python3
"""Fresh, offscreen sample captures only. No bundle, helper or live backend."""
import argparse, hashlib, json, os, pathlib, shutil, subprocess, tempfile, time
from PIL import Image
ROOT = pathlib.Path(__file__).resolve().parents[3]
OUT = ROOT / 'docs/pdf/assets'
# Only scenes used by the merged journey; no duplicate appearances or unused panes.
SCENES = [
 ('home','h2-home'), ('trigger','home-watch'), ('map-hover','people-map-hover'),
 ('person','person-full'), ('circle','circle'), ('activity','u4-activity'),
 ('effort','a6-named-effort'), ('detailed-log','u4-activity-log'),
 ('integration-access','remote-approval-work'), ('about-you','s2-about-settings'),
 ('hidden-people','s2-hidden-people'), ('menubar','menubar'),
 ('welcome','setup-welcome'), ('pairing','setup-qr'), ('syncing','setup-syncing'),
 ('ready','setup-done'), ('helper','o1-helper-registering'), ('link-error','o1-link-error'),
 ('suggestions','proposals-review'), ('ai-picker','connect-picker'),
 ('chatgpt','connect-chatgpt'), ('send-approval','approval'),
 ('naming','n1-naming-activity'), ('updates','update-notes')]


# Coordinates are in the source snapshot's logical points. Crop at the
# corresponding native-scale pixel boundaries; never resize or redraw screenshots.
DETAILS = {
 'welcome-panel': ('welcome-light', (420, 230, 1020, 710)),
 'helper-panel': ('helper-light', (420, 230, 1020, 710)),
 'pairing-panel': ('pairing-light', (420, 150, 1020, 800)),
 'error-panel': ('link-error-light', (420, 230, 1020, 710)),
 'syncing-panel': ('syncing-light', (420, 260, 1020, 710)),
 'ready-panel': ('ready-light', (420, 260, 1020, 710)),
 'picker-panel': ('ai-picker-light', (470, 260, 990, 560)),
 'chatgpt-panel': ('chatgpt-light', (400, 100, 1040, 800)),
 'access-card': ('integration-access-light', (420, 120, 1020, 780)),
 'send-panel': ('send-approval-light', (400, 230, 1040, 535)),
 'menu-panel': ('menubar-light', (525, 140, 915, 760)),
 'watch-card': ('trigger-light', (641, 354, 979, 549)),
 'suggestions-panel': ('suggestions-light', (400, 185, 1040, 730)),
 'home-focus': ('home-light', (195, 390, 1110, 835)),
 'map-focus': ('map-hover-light', (420, 55, 1210, 510)),
 'circle-focus': ('circle-light', (415, 15, 1210, 705)),
 'person-focus': ('person-light', (420, 40, 1210, 725)),
 'about-focus': ('about-you-light', (205, 325, 920, 900)),
 'activity-focus': ('activity-light', (295, 25, 1145, 615)),
 'effort-focus': ('effort-light', (445, 50, 1020, 735)),
 'naming-focus': ('naming-light', (385, 25, 1230, 675)),
 'log-focus': ('detailed-log-light', (295, 25, 1145, 760)),
 'hidden-focus': ('hidden-people-light', (195, 20, 930, 785)),
 'updates-panel': ('updates-light', (400, 180, 1040, 740)),
}

def make_details():
    directory = OUT/'details'; directory.mkdir(parents=True, exist_ok=True)
    records = []
    for name, (source, points) in DETAILS.items():
        original = OUT/(source+'.png')
        record = next(r for r in json.loads((OUT/'manifest.json').read_text())['captures'] if r['file']==original.name)
        scale = record['scale']
        pixels = tuple(v*scale for v in points)
        with Image.open(original) as image:
            if image.size != tuple(record['pixels']): raise RuntimeError('Native source dimensions changed')
            crop = image.crop(pixels)
            destination = directory/(name+'.png'); crop.save(destination)
            records.append(dict(file=destination.name, source=original.name,
                source_sha256=hashlib.sha256(original.read_bytes()).hexdigest(),
                crop_points=list(points), crop_pixels=list(pixels), pixels=list(crop.size),
                sha256=hashlib.sha256(destination.read_bytes()).hexdigest(),
                resampled=False))
    (directory/'manifest.json').write_text(json.dumps(dict(schema=1,
        operation='lossless native-pixel crop; no resampling or image generation',
        details=records), indent=2)+'\n')
    print(f'{len(records)} native-pixel detail crops written', flush=True)


def retina_binary(scale):
    # Offscreen AppKit has 1x backing in this sandbox. Allocate native scaled pixels
    # in a disposable source copy, preserving the original 1440x900 point layout.
    cache = ROOT/'docs/pdf/.cache/capture'; cache.mkdir(parents=True, exist_ok=True)
    package = cache/'app'
    if package.exists(): shutil.rmtree(package)
    package.mkdir()
    shutil.copy2(ROOT/'app/Package.swift', package/'Package.swift')
    shutil.copytree(ROOT/'app/Sources', package/'Sources')
    shutil.copytree(ROOT/'app/Tests', package/'Tests')
    source = package/'Sources/Limmr/SnapshotRunner.swift'
    original = 'bitmapDataPlanes: nil, pixelsWide: Int(size.width), pixelsHigh: Int(size.height),'
    replacement = f'bitmapDataPlanes: nil, pixelsWide: Int(size.width * {scale}), pixelsHigh: Int(size.height * {scale}),'
    text = source.read_text()
    if text.count(original) != 1: raise RuntimeError('Snapshot renderer changed; review scaled allocation before capture')
    source.write_text(text.replace(original, replacement))
    env = dict(os.environ, CLANG_MODULE_CACHE_PATH=str(cache/'clang'),
               SWIFTPM_MODULECACHE_OVERRIDE=str(cache/'swift'))
    with tempfile.TemporaryDirectory(prefix='build-home-', dir=cache) as home:
        env['LIMMR_HOME'] = home
        command = ['swift','build','--product','Limmr','--package-path',str(package),'--disable-sandbox',
                   '--scratch-path',str(cache/'build'), '--cache-path',str(cache/'swiftpm'),
                   '--config-path',str(cache/'config'),'--security-path',str(cache/'security')]
        with (cache/'build.log').open('w') as log:
            subprocess.run(command, env=env, stdin=subprocess.DEVNULL, stdout=log,
                           stderr=subprocess.STDOUT, check=True, timeout=300)
    return cache/'build/debug/Limmr'

def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--binary', type=pathlib.Path, help='Existing unbundled binary whose native pixel scale matches --scale')
    p.add_argument('--scale', type=int, choices=(2,3), default=3, help='Native capture bitmap scale, never image upscaling')
    p.add_argument('--dark', action='store_true', help='Also capture dark appearance')
    p.add_argument('--details-only', action='store_true', help='Crop existing native captures; no app build or launch')
    p.add_argument('--only', help='Comma-separated scene names; existing manifest is merged')
    args = p.parse_args()
    if args.details_only:
        make_details()
        return
    binary = args.binary.resolve() if args.binary else retina_binary(args.scale).resolve()
    if '.app' in binary.parts or any(part.endswith('.app') for part in binary.parts):
        p.error('Use the unbundled SwiftPM executable; bundled apps are forbidden.')
    if not binary.is_file(): p.error('Build the unbundled executable first (see README).')
    chosen = set(args.only.split(',')) if args.only else {s[0] for s in SCENES}
    if chosen - {s[0] for s in SCENES}: p.error('Unknown scene name')
    OUT.mkdir(parents=True, exist_ok=True)
    cache = ROOT/'docs/pdf/.cache/capture'; cache.mkdir(parents=True, exist_ok=True)
    manifest_path = OUT/'manifest.json'
    old = json.loads(manifest_path.read_text()) if args.only and manifest_path.exists() else {}
    records = {r['file']:r for r in old.get('captures',[])}
    binary_hash = hashlib.sha256(binary.read_bytes()).hexdigest()
    for name, page in SCENES:
        if name not in chosen: continue
        logical_size = (1440, 900)
        for mode in (('light','dark') if args.dark else ('light',)):
            path = OUT/f'{name}-{mode}.png'
            with tempfile.TemporaryDirectory(prefix='home-', dir=cache) as home:
                env = dict(os.environ, LIMMR_HOME=home)
                command = [str(binary),'--snapshot',str(path),'--page',page,'--size', 'x'.join(map(str, logical_size))]
                if mode == 'dark': command += ['--dark']
                run = subprocess.run(command, env=env, stdin=subprocess.DEVNULL,
                                     stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=45)
                if run.returncode: raise RuntimeError(f'{name}/{mode}: snapshot failed ({run.returncode}); no live retry attempted')
            with Image.open(path) as im:
                if im.size != tuple(v*args.scale for v in logical_size):
                    raise RuntimeError(f'{path.name}: expected native {args.scale}x, got {im.size}; no upscaling')
            records[path.name] = dict(file=path.name, page=page, appearance=mode,
                logical_size=list(logical_size), pixels=[v*args.scale for v in logical_size], scale=args.scale,
                sha256=hashlib.sha256(path.read_bytes()).hexdigest(), binary_sha256=binary_hash,
                captured_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()))
            print(f'{name}-{mode}: native {args.scale}x', flush=True)
    manifest_path.write_text(json.dumps(dict(schema=1, data='invented DemoBackend only', renderer='AppKit native bitmap at requested scale; capture-only allocation patch in disposable source copy', source_revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=ROOT,stdin=subprocess.DEVNULL,text=True).strip(),
        captures=sorted(records.values(),key=lambda r:r['file'])),indent=2)+'\n')
    if all((OUT/(source+'.png')).exists() for source,_ in DETAILS.values()):
        make_details()
if __name__ == '__main__': main()
