from pathlib import Path
from PIL import Image, ImageDraw

folder = Path(__file__).resolve().parent.parent / 'build/snapshots/a9'
files = sorted(folder.glob('*-light.png')) + sorted(folder.glob('*-dark.png'))
width, height = 400, 302
sheet = Image.new('RGB', (width * 4, height * ((len(files) + 3) // 4)), '#d8d8d8')
draw = ImageDraw.Draw(sheet)
for index, path in enumerate(files):
    with Image.open(path) as source:
        image = source.convert('RGB')
        image.thumbnail((width - 12, height - 30))
        x, y = (index % 4) * width, (index // 4) * height
        sheet.paste(image, (x + 6, y + 24))
        draw.text((x + 6, y + 6), path.stem, fill='#222222')
sheet.save(folder / 'contact-sheet.jpg', quality=90)

# Four-frame sheets retain readable text for reviewing each page at both sizes/appearances.
review = folder / 'review'
review.mkdir(exist_ok=True)
pages = sorted({path.name.rsplit('-', 2)[0] for path in files})
for page in pages:
    frame = Image.new('RGB', (1600, 1240), '#cccccc')
    draw = ImageDraw.Draw(frame)
    for index, (size, appearance) in enumerate((('1100x740', 'light'), ('1100x740', 'dark'), ('800x600', 'light'), ('800x600', 'dark'))):
        path = folder / f'{page}-{size}-{appearance}.png'
        with Image.open(path) as source:
            image = source.convert('RGB')
            image.thumbnail((800, 600))
            x, y = (index % 2) * 800, (index // 2) * 620
            draw.text((x + 8, y + 4), path.name, fill='#202020')
            frame.paste(image, (x, y + 20))
    frame.save(review / f'{page}.jpg', quality=94)
