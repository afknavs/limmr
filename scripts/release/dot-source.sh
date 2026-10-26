#!/bin/sh
# All tracked files, including integrations and shared Swift task fixtures, are
# needed: engine tests deliberately audit those distribution contracts.
set -eu
root=$(git rev-parse --show-toplevel)
output=${1:-"$root/dist/limmr-dot-source.tar.gz"}
python3 - "$root" "$output" <<'PY'
import hashlib
from pathlib import Path
import subprocess
import sys
import tarfile
root, output = Path(sys.argv[1]), Path(sys.argv[2]).resolve()
files = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z']).split(b'\0')
output.parent.mkdir(parents=True, exist_ok=True)
with tarfile.open(output, 'w:gz') as archive:
    for raw in files:
        if not raw:
            continue
        path = Path(raw.decode())
        if not (root / path).is_file():
            raise SystemExit(f'Missing tracked source file: {path}')
        archive.add(root / path, arcname=str(Path('limmr-dot-source') / path), recursive=False)
checksum = hashlib.sha256(output.read_bytes()).hexdigest()
output.with_name(output.name + '.sha256').write_text(f'{checksum}  {output.name}\n')
print(f'Created {output.name} and SHA-256 checksum')
PY
