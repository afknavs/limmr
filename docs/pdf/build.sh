#!/bin/bash
# Local-only sample capture and A4 printing. Never launches a bundle or installs a helper.
set -euo pipefail
exec < /dev/null
PDF_ROOT="$(cd "$(dirname "$0")" && pwd)"
CHROME_BIN="${CHROME_BIN:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
# Rebuild sample windows from current app sources unless explicitly reusing captures.
# The bitmap allocation patch exists only in docs/pdf/.cache/capture/app.
if [ "${1:-}" = --setup-guide ]; then
  if [ "${2:-}" != --reuse-captures ]; then
    nice -n 15 python3 "$PDF_ROOT/scripts/setup-guide.py" --capture
  fi
  python3 "$PDF_ROOT/scripts/setup-guide.py"
  DOC_NAMES=(setup-guide)
else
DOC_NAMES=(what-limmr-is how-to-pitch-it)
case "${1:-}" in
  "") python3 "$PDF_ROOT/scripts/capture.py" --scale 3 ;;
  --reuse-captures) python3 "$PDF_ROOT/scripts/capture.py" --details-only ;;
  *) echo 'Usage: build.sh [--reuse-captures]' >&2; exit 2 ;;
esac
python3 "$PDF_ROOT/scripts/verify.py"
fi
if [ ! -x "$CHROME_BIN" ]; then
  echo 'Set CHROME_BIN to a local Chrome/Chromium executable.' >&2
  exit 1
fi
# Complete native sources, lossless crops and physical-width ppi checked above.
mkdir -p "$PDF_ROOT/output" "$PDF_ROOT/.cache/print"
PRINT_PROFILE="$(mktemp -d "$PDF_ROOT/.cache/print/profile.XXXXXX")"
trap 'rm -rf "$PRINT_PROFILE"' EXIT
for DOC_NAME in "${DOC_NAMES[@]}"; do
  # Inspect actual print-size DOM bounds before producing each PDF.
  "$CHROME_BIN" --headless --disable-gpu --no-first-run --no-default-browser-check \
    --disable-extensions --disable-background-networking --disable-sync \
    --metrics-recording-only --allow-file-access-from-files \
    --user-data-dir="$PRINT_PROFILE" --virtual-time-budget=30000 --dump-dom \
    "file://$PDF_ROOT/src/$DOC_NAME.html" \
    > "$PDF_ROOT/.cache/print/$DOC_NAME-layout.html"
  python3 "$PDF_ROOT/scripts/verify.py" \
    --layout-dom "$PDF_ROOT/.cache/print/$DOC_NAME-layout.html"
  "$CHROME_BIN" --headless --disable-gpu --no-first-run --no-default-browser-check \
    --disable-extensions --disable-background-networking --disable-sync \
    --metrics-recording-only --no-pdf-header-footer --allow-file-access-from-files \
    --user-data-dir="$PRINT_PROFILE" --timeout=20000 \
    --print-to-pdf="$PDF_ROOT/output/.$DOC_NAME.pdf" \
    "file://$PDF_ROOT/src/$DOC_NAME.html"
  test -s "$PDF_ROOT/output/.$DOC_NAME.pdf"
  if [ "$DOC_NAME" = setup-guide ]; then
    mv "$PDF_ROOT/output/.$DOC_NAME.pdf" "$PDF_ROOT/Limmr-Setup-Guide.pdf"
  else
    mv "$PDF_ROOT/output/.$DOC_NAME.pdf" "$PDF_ROOT/output/$DOC_NAME.pdf"
  fi
done

if [ "${1:-}" = --setup-guide ]; then
  python3 "$PDF_ROOT/scripts/setup-guide.py" --pdf
else
  python3 "$PDF_ROOT/scripts/verify.py" --pdfs
fi
