#!/usr/bin/env python3
"""D3: local assets, native pixels, physical print resolution and optional render checks."""
import argparse
import hashlib
import json
from html.parser import HTMLParser
from pathlib import Path
import re
import subprocess
from PIL import Image

ROOT = Path(__file__).resolve().parents[1]
EXPECTED = {'what-limmr-is': 26, 'how-to-pitch-it': 4, 'setup-guide': 11}
MIN_PPI = 220
MIN_UI_PT = 6.75  # approximately 7 pt for the declared source UI reference text


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


class Document(HTMLParser):
    def __init__(self, path):
        super().__init__()
        self.path = path
        self.pages = self.images = self.captions = self.underneath = 0
        self.heading = None
        self.headings = []
        self.fig_width = None
        self.placements = []
        self.page_images = []
        self.snapshots = []

    def handle_starttag(self, tag, attributes):
        a = dict(attributes)
        classes = a.get('class', '').split()
        if tag == 'section' and 'page' in classes:
            self.pages += 1
            self.page_images.append(0)
            self.snapshots.append(a.get('data-snapshot'))
        if tag == 'aside' and 'underneath' in classes:
            self.underneath += 1
        if tag in ('h1', 'h2', 'h3'):
            self.heading = ''
        if tag == 'figure':
            width = re.search(r'--print-width:\s*([\d.]+)mm', a.get('style', ''))
            self.fig_width = float(width[1]) if width else None
        if tag == 'img':
            self.images += 1
            self.page_images[-1] += 1
            assert a.get('alt'), 'Image needs context alt text'
            with Image.open(self.path.parent / a['src']) as image:
                assert image.size == (int(a['width']), int(a['height'])), a['src']
                # Explicit physical widths also allow the check when Chrome is blocked.
                assert self.fig_width and 0 < self.fig_width <= 174, a['src']
                ppi = image.width * 25.4 / self.fig_width
                assert ppi >= MIN_PPI, f"{a['src']}: {ppi:.1f} ppi < {MIN_PPI}"
                logical_width = float(a['data-logical-width'])
                reference_pt = float(a['data-ui-reference-pt'])
                assert logical_width == image.width / 3, 'Current tour uses native 3x crops'
                ui_pt = reference_pt * self.fig_width * 72 / 25.4 / logical_width
                assert ui_pt >= MIN_UI_PT, f"{a['src']}: reference UI text only {ui_pt:.2f} pt"
                assert '/details/' in a['src'] or '/setup/' in a['src'], 'Use a focused lossless crop, not the full window'
                self.placements.append(dict(page=self.pages, image=a['src'],
                    width_mm=self.fig_width, ppi=round(ppi, 2), pixels=list(image.size),
                    logical_width=logical_width, ui_reference_pt=reference_pt,
                    max_width_ui_pt=round(ui_pt, 2)))
        if tag == 'figcaption':
            self.captions += 1
        for key in ('href', 'src'):
            if ref := a.get(key):
                assert '://' not in ref and not ref.startswith('//'), 'Local resources only'
                assert (self.path.parent / ref.split('#')[0]).exists(), ref

    def handle_endtag(self, tag):
        if tag in ('h1', 'h2', 'h3') and self.heading is not None:
            self.headings.append(self.heading.strip())
            self.heading = None
        if tag == 'figure':
            self.fig_width = None

    def handle_data(self, data):
        if self.heading is not None:
            self.heading += data


def verify_sources():
    capture_manifest = json.loads((ROOT / 'assets/manifest.json').read_text())
    captures = {row['file']: row for row in capture_manifest['captures']}
    assert capture_manifest['data'] == 'invented DemoBackend only'
    for filename, row in captures.items():
        path = ROOT / 'assets' / filename
        assert digest(path) == row['sha256'], filename
        assert row['scale'] in (2, 3)
        with Image.open(path) as image:
            assert image.size == tuple(row['pixels'])
            assert list(image.size) == [v * row['scale'] for v in row['logical_size']]
    details = json.loads((ROOT / 'assets/details/manifest.json').read_text())['details']
    for row in details:
        source = ROOT / 'assets' / row['source']
        path = ROOT / 'assets/details' / row['file']
        assert digest(source) == row['source_sha256'] and digest(path) == row['sha256']
        assert not row['resampled']
        scale = captures[row['source']]['scale']
        assert row['crop_pixels'] == [v * scale for v in row['crop_points']]
        with Image.open(source) as original, Image.open(path) as detail:
            bounds = row['crop_pixels']
            assert 0 <= bounds[0] < bounds[2] <= original.width
            assert 0 <= bounds[1] < bounds[3] <= original.height
            native = original.crop(tuple(bounds))
            assert detail.size == native.size and detail.tobytes() == native.tobytes(), path
    results = {}
    for name, count in EXPECTED.items():
        if name == 'setup-guide': continue
        path = ROOT / 'src' / f'{name}.html'
        doc = Document(path)
        doc.feed(path.read_text())
        assert doc.pages == count and doc.images == doc.captions, name
        if name == 'what-limmr-is':
            assert doc.page_images == [1] * count, 'One screen per journey page'
            assert doc.underneath == count and all(doc.snapshots)
        results[name] = dict(pages=count, images=doc.images, headings=doc.headings,
            snapshots=doc.snapshots, placements=doc.placements, sha256=digest(path))
    css = (ROOT / 'src/style.css').read_text()
    assert not re.search(r'(?:letter|word)-spacing\s*:\s*-', css), 'Negative tracking'
    assert 'letter-spacing:normal; word-spacing:normal' in css
    assert '.journey-screen img { width:var(--render-width,var(--print-width)); max-width:100%;' in css
    assert '.journey .body { justify-content:flex-start; gap:14pt; }' in css
    for old in ('what-it-looks-like', 'how-it-works'):
        assert not (ROOT / 'src' / f'{old}.html').exists(), 'Remove superseded source'
        assert not (ROOT / 'output' / f'{old}.pdf').exists(), 'Remove superseded output'
    placements = [p for doc in results.values() for p in doc['placements']]
    print(f"PASS: {len(captures)} native captures; {len(details)} pixel-identical crops; "
          f"{sum(EXPECTED.values())} source pages; local links, captions and headings")
    print(f"PASS: {len(placements)} image placements; minimum {min(p['ppi'] for p in placements):.2f} ppi "
          f"at declared physical widths (required {MIN_PPI})")
    return results


class LayoutReport(HTMLParser):
    def __init__(self):
        super().__init__()
        self.active = False
        self.text = ''

    def handle_starttag(self, tag, a):
        if tag == 'script' and dict(a).get('id') == 'layout-report':
            self.active = True

    def handle_endtag(self, tag):
        if tag == 'script':
            self.active = False

    def handle_data(self, data):
        if self.active:
            self.text += data


def verify_layout(path):
    report = LayoutReport()
    report.feed(path.read_text())
    assert report.text, 'Chrome layout report missing; wait for fonts and images'
    pages = json.loads(report.text)
    name = path.name.removesuffix('-layout.html')
    assert name in EXPECTED and len(pages) == EXPECTED[name], 'Wrong rendered page count'
    for page in pages:
        assert not page['overflow'] and not page['brokenImages'], page
        if page['journey']:
            assert 2 <= page['gapToFooterMm'] and page['blankEndRatio'] <= 0.27, page
            assert len(page['textGapsPt']) == 3, page
            assert all(12 <= gap <= 16 for gap in page['textGapsPt']), page
        elif name == 'setup-guide':
            assert 2 <= page['gapToFooterMm'], page
        else:
            assert 2 <= page['gapToFooterMm'] <= 35, page
        for heading in page['headings']:
            assert heading['tracking'] in ('normal', '0px')
            assert heading['wordSpacing'] in ('normal', '0px')
        for image in page['images']:
            assert min(image['xPpi'], image['yPpi']) >= MIN_PPI, image
            if page['journey']:
                assert image['uiTextPt'] >= MIN_UI_PT, image
        assert not page['orphanHeadings'], page
    print(f'PASS: {len(pages)} rendered pages; resolution, spacing, headings and bounds')


def verify_pdfs():
    import fitz
    tour = Document(ROOT / 'src/what-limmr-is.html')
    tour.feed(tour.path.read_text())
    for name, expected in EXPECTED.items():
        if name == 'setup-guide': continue
        path = ROOT / 'output' / f'{name}.pdf'
        assert path.exists(), f'Export missing: {path.name}'
        with fitz.open(path) as pdf:
            assert len(pdf) == expected, (name, len(pdf), expected)
            raster_count = 0
            for page in pdf:
                assert abs(page.rect.width - 595) < 2 and abs(page.rect.height - 842) < 2
                assert ('D3 R2' if name == 'what-limmr-is' else 'Round 2') in page.get_text(), 'Stale pre-review PDF'
                for image in page.get_image_info():
                    x0, y0, x1, y1 = image['bbox']
                    ppi = min(image['width'] * 72 / (x1-x0), image['height'] * 72 / (y1-y0))
                    assert ppi >= MIN_PPI, (name, page.number + 1, ppi)
                    if name == 'what-limmr-is':
                        placement = tour.placements[page.number]
                        assert [image['width'], image['height']] == placement['pixels'], 'Wrong crop in PDF'
                        ui_pt = placement['ui_reference_pt'] * (x1-x0) / placement['logical_width']
                        assert ui_pt >= MIN_UI_PT, (name, page.number + 1, 'UI text too small', ui_pt)
                    raster_count += 1
            if name == 'what-limmr-is':
                assert raster_count >= expected
        listing = subprocess.check_output(['pdfimages', '-list', str(path)], text=True)
        for line in listing.splitlines()[2:]:
            fields = line.split()
            if len(fields) >= 16 and fields[2] in ('image', 'mask', 'smask'):
                assert min(float(fields[12]), float(fields[13])) >= MIN_PPI, line
        print(f'PASS: {path.name}: {expected} A4 pages; all embedded images >= {MIN_PPI} ppi')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--layout-dom', type=Path)
    parser.add_argument('--pdfs', action='store_true')
    args = parser.parse_args()
    if args.layout_dom and args.layout_dom.name == 'setup-guide-layout.html':
        verify_layout(args.layout_dom)
        return
    results = verify_sources()
    if args.layout_dom:
        verify_layout(args.layout_dom)
    if args.pdfs:
        verify_pdfs()
    manifest = dict(round=3, edition="D3 R2", documents=results, min_required_ppi=MIN_PPI,
        min_reference_ui_pt=MIN_UI_PT,
        stylesheet_sha256=digest(ROOT / 'src/style.css'),
        render_status=('PDF sizes, counts and embedded ppi checked; visual page review pending'
                       if args.pdfs else 'Source and physical-width ppi checked; Chrome print and visual review pending'))
    (ROOT / 'source-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')


if __name__ == '__main__':
    main()
