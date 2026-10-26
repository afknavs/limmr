#!/bin/sh
# Excludes private integration guides and audits every exported file.
# Local preparation only. No remote, publication, installation or credentials.
set -eu
exec python3 "$(dirname "$0")/public-export.py" "$@"
