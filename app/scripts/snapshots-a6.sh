#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
binary=${1:-.build/debug/Limmr}
output=build/snapshots/a6
mkdir -p "$output"
pages=(home people-circles people-people people-map-hover people-replay)
for size in 1100x740 800x600; do
    for page in $pages; do
        for appearance in light dark; do
            extra=()
            [[ "$appearance" == dark ]] && extra=(--dark)
            "$binary" --snapshot "$output/$page-$size-$appearance.png" --page "$page" --size "$size" "${extra[@]}" < /dev/null 2>"$output/render.log"
            echo "$page-$size-$appearance.png"
        done
    done
done
"$binary" --snapshot ../docs/images/people.png --page people-circles --size 1280x800 < /dev/null 2>"$output/render.log"
echo "docs/images/people.png"
