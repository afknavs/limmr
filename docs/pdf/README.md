# Limmr documents

D3 merges the former tour and architecture documents into **What Limmr is**. It follows a person’s first use of Limmr, from installation through linking, Home, People, AI connections, sending, Activity, privacy and updates. The pitch is a separate short introduction.

| Document | Source | A4 pages |
| --- | --- | ---: |
| What Limmr is | [HTML](src/what-limmr-is.html) | 26 |
| How to pitch it | [HTML](src/how-to-pitch-it.html) | 4 |

D3 R2 crops every image to the content that carries its point; full-window backgrounds no longer consume the frame. Home uses the circle grid as its focused detail, while Activity, People, About you and hidden chats use their content columns. Each journey page contains one lossless native crop, a 2–4 sentence caption, a short practical explanation and a 2–3 sentence **Underneath** note. It explains the relevant engine, storage, AI access or MCP behaviour at the point where you meet it. The AI lookup page shows its source context and describes a possible returned result; it does not invent an AI-client screenshot or claim a live lookup.

## Rebuild from current app sources

```sh
bash docs/pdf/build.sh < /dev/null
```

The build first captures fresh invented-data screens, then verifies sources and resolution, measures layout with Chrome and prints these outputs:

- `output/what-limmr-is.pdf`
- `output/how-to-pitch-it.pdf`

It never launches a bundled app, installs a helper, starts an engine or changes client configuration. The capture script copies current app sources to ignored `docs/pdf/.cache/capture/app/`, changes **only the offscreen bitmap allocation** to 3× there, builds the unbundled executable and invokes its `--snapshot` path with closed stdin and a new throwaway `LIMMR_HOME` for each capture. Tracked `app/` and `engine/` remain unchanged. Nothing is downloaded.

The logical layout remains 1440 × 900 points; native captures are **4320 × 2700 pixels**. `--scale 3` belongs to the capture script; it does not add a new app CLI flag. Pixel cropping and resolution are independent of the app’s current Home or People layout. Full screens are recaptured from the current source, so later H3/P10 app work is reflected automatically. Fixed panel crop bounds are centralised in `scripts/capture.py` and must be checked if those panels change.

Capture separately, or crop an existing capture set:

```sh
python3 docs/pdf/scripts/capture.py --scale 3 < /dev/null
python3 docs/pdf/scripts/capture.py --details-only < /dev/null
```

For a repeat print without rebuilding the captures (this also regenerates all scripted crops):

```sh
bash docs/pdf/build.sh --reuse-captures < /dev/null
```

Use `CHROME_BIN` for a different local Chrome/Chromium installation. All resources are local. Profiles and review intermediates stay in `docs/pdf/.cache/`; generated captures and PDFs are ignored by Git. The superseded `what-it-looks-like` and `how-it-works` sources and current outputs are removed. Older archived files, if present in another checkout, are historical evidence rather than D3 exports.

## Sharpness and layout gates

```sh
python3 docs/pdf/scripts/verify.py < /dev/null
python3 docs/pdf/scripts/verify.py --pdfs < /dev/null
```

The source check verifies capture hashes, native dimensions, crop bounds and pixel-for-pixel identity, local resource links, intrinsic dimensions, captions, page counts and headings. It checks **every image placement at 220 ppi or higher** using its explicit physical print width. CSS and the print layout script can shrink a figure to fit, but cannot stretch it beyond that width. The 130 mm image-height cap is removed: after fonts and images settle, the layout script gives the image the available vertical space, up to its physical-width/ppi limit. The caption, practical block, Underneath and source use fixed **14 pt** gaps rather than distributed free space. This check runs before Chrome, even when Chrome cannot start.

During printing the DOM checker measures actual image dimensions and both horizontal and vertical ppi, heading spacing, missing images, orphan headings, footer collisions, horizontal overflow and excessive end gaps. Journey pages also require 12–16 pt measured gaps between text blocks, at most 25% blank space between the last content and footer, and reference UI text of at least 6.75 pt (approximately 7 pt). This estimate uses the app’s 13 pt row/body labels, or the map’s explicit 11 pt circle labels; smaller metadata remains secondary. Source checks calculate the reference size at the maximum width; DOM and PDF checks use the actual fitted width. A failed layout check stops the build. After printing, `verify.py --pdfs` checks A4 dimensions, page counts and current edition headers (`D3 R2` for the journey), measures each embedded raster’s actual PDF rectangle, and independently checks `pdfimages -list`; any image below 220 ppi fails the build. The PDF check also confirms the expected crop dimensions and fitted reference UI size. Pre-review D3 PDFs are retained as a baseline and fail the current edition check until reprinted. Source-only ppi is not a substitute for that final embedded-image check.

After a successful print, render **every page** and inspect the resulting PNGs:

```sh
mkdir -p docs/pdf/.cache/review
pdftoppm -scale-to 1800 -png docs/pdf/output/what-limmr-is.pdf docs/pdf/.cache/review/what-limmr-is
pdftoppm -scale-to 1800 -png docs/pdf/output/how-to-pitch-it.pdf docs/pdf/.cache/review/how-to-pitch-it
```

Check the full-page composition and zoom in on QR, approval, menu and update panels. Automated checks cannot establish visual polish. `source-manifest.json` records source hashes, page inventory, image placements and source ppi. [D3 report](../D3-REPORT.md) records the checks actually completed and the page-to-snapshot list. Claude printed the first D3 edition outside the sandbox and reported 26 pages with all images at least 220 ppi. The revised D3 R2 sources and crops require a new outside-sandbox print and page review; the existing PDFs are the previous edition. Chrome is not retried here.


## Beta setup guide (Z3)

The eleven-page [Limmr Setup Guide](Limmr-Setup-Guide.pdf) is a separate light-theme first-run guide.
Its [HTML source](src/setup-guide.html) shares `src/style.css` and `scripts/layout-check.js` with the existing documents.

```sh
nice -n 15 bash docs/pdf/build.sh --setup-guide < /dev/null
# Reprint without rebuilding/capturing the app:
nice -n 15 bash docs/pdf/build.sh --setup-guide --reuse-captures < /dev/null
```

The setup route captures only its own eight scenes from invented DemoBackend data: six guide illustrations,
a dark copy-prompt view, and a hosted copy-prompt view. Native 3x lossless crops and their hashes live in
`docs/setup/`; intermediate full windows stay in `.cache/setup/`. Each invocation uses a throwaway home.
The setup verifier checks local resources, image dimensions, captions, eleven source pages, print resolution,
then the common layout gate and eleven A4 PDF pages with embedded images at 220 ppi or better.
The PDF is copied byte-for-byte to `app/Resources/Limmr-Setup-Guide.pdf`; `app/build.sh` bundles that resource.
The public exporter explicitly retains the guide and `docs/setup/` PNG/JPEG images. Other documentation
rasters remain excluded. Re-render and visually inspect every page after changing the guide.
