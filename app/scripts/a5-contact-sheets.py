#!/usr/bin/env python3
"""Make labelled review sheets without altering the original A5 captures."""
from pathlib import Path
import sys
from PIL import Image, ImageDraw, ImageFont
ROOT = Path(__file__).resolve().parents[2]
stage = sys.argv[1]
folder = ROOT / 'docs/snapshots/A5' / stage
out = folder / 'review'
out.mkdir(exist_ok=True)
font = ImageFont.truetype('/System/Library/Fonts/Helvetica.ttc', 18)
names = sorted({p.stem.rsplit('-', 2)[0] for p in folder.glob('*.png')})
for name in names:
    # Every capture is included, with enough size to read at original resolution.
    sheet = Image.new('RGB', (2260, 2300 if name in ('person', 'circle') else 1560), '#dddddd')
    draw = ImageDraw.Draw(sheet)
    for row, theme in enumerate(('light', 'dark')):
        for col, width in enumerate((760, 1100, 1600)):
            p = folder / f'{name}-{width}-{theme}.png'
            if not p.exists():
                continue
            im = Image.open(p).convert('RGB')
            # Review the detail column at native scale, omit empty viewport below content.
            # Original full-size PNGs remain alongside this sheet.
            if name in ('person', 'circle', 'home', 'home-empty-triggers', 'home-quiet', 'people-list'):
                im = im.crop((180 if not name.startswith('home-quiet') else 180, 0, width, 1800))
            else:
                im = im.crop((0, 0, width, min(im.height, 1150)))
            im.thumbnail((740, sheet.height // 2 - 40))
            x, y = col * 750, row * (sheet.height // 2)
            draw.text((x + 8, y + 4), f'{name} / {width} pt / {theme}', fill='black', font=font)
            sheet.paste(im, (x + 4, y + 32))
    sheet.save(out / f'{name}.jpg', quality=90)
print(f'{len(names)} review sheets: {out}')
