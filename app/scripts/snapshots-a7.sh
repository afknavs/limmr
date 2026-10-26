#!/bin/zsh
set -euo pipefail
cd "${0:A:h}/.."
export LIMMR_HOME=$(mktemp -d)
binary=${1:-.build/debug/Limmr}
output=../notes/a7
mkdir -p "$output"
pages=(person person-review person-forget person-add home people-people people-fill settings-general settings-sources settings-integrations settings-integrations-access integration-detail settings-hidden settings-remote settings-health settings-advanced)
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
for appearance in light dark; do
  extra=()
  [[ "$appearance" == dark ]] && extra=(--dark)
  "$binary" --snapshot "$output/home-1280x800-$appearance.png" --page home --size 1280x800 "${extra[@]}" < /dev/null 2>"$output/render.log"
  "$binary" --snapshot "$output/person-full-1100x1200-$appearance.png" --page person-full --size 1100x1200 "${extra[@]}" < /dev/null 2>"$output/render.log"
  "$binary" --snapshot "$output/settings-sources-1100x1000-$appearance.png" --page settings-sources --size 1100x1000 "${extra[@]}" < /dev/null 2>"$output/render.log"
done
