#!/bin/sh
# usage: shot.sh <page> <query> <outname>   e.g. shot.sh home "m=dark&t=paper" home-paper-dark
cd "$(dirname "$0")"
"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" --headless=new --disable-gpu --hide-scrollbars --user-data-dir=/private/tmp/n4-chrome --force-device-scale-factor=2 --window-size=1280,800 --virtual-time-budget=4000 --screenshot="$PWD/out/$3.png" "file://$PWD/$1.html?$2" >/dev/null 2>&1
ls -la "out/$3.png" | awk '{print $5, $9}'
