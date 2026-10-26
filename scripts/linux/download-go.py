#!/usr/bin/env python3
"""Fetch the exact go.mod toolchain; validate official metadata before unpacking."""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import sys
import tarfile
import tempfile
import urllib.request


def install(version):
    arch = {'x86_64': 'amd64', 'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine())
    if not arch:
        raise ValueError('Supported Linux architectures: amd64 and arm64')
    filename = f'go{version}.linux-{arch}.tar.gz'
    with urllib.request.urlopen('https://go.dev/dl/?mode=json&include=all', timeout=60) as response:
        releases = json.load(response)
    entry = next((f for r in releases if r['version'] == f'go{version}'
                  for f in r['files'] if f['filename'] == filename), None)
    if entry is None:
        raise ValueError('Required Go release is absent from official metadata; set LIMMR_GO')
    parent = Path.home() / '.local'
    parent.mkdir(parents=True, exist_ok=True)
    target = parent / 'limmr-go'
    # Never overwrite an existing installation, even an old or unrelated one.
    if target.exists():
        raise ValueError('~/.local/limmr-go already exists; set LIMMR_GO to a newer toolchain')
    with tempfile.TemporaryDirectory(prefix='.limmr-go-', dir=parent) as temp:
        archive = Path(temp) / filename
        with urllib.request.urlopen(f'https://go.dev/dl/{filename}', timeout=60) as response, archive.open('wb') as out:
            shutil.copyfileobj(response, out)
        digest = hashlib.sha256()
        with archive.open('rb') as source:
            for block in iter(lambda: source.read(1024 * 1024), b''):
                digest.update(block)
        if digest.hexdigest() != entry['sha256']:
            raise ValueError('Official Go archive SHA-256 mismatch; nothing installed')
        with tarfile.open(archive) as tar:
            # The verified Go archive contains only its go/ directory.
            for member in tar.getmembers():
                path = Path(member.name)
                if path.is_absolute() or '..' in path.parts or path.parts[0] != 'go' or member.issym() or member.islnk():
                    raise ValueError('Unsafe Go archive member; nothing installed')
            tar.extractall(temp)
        os.rename(Path(temp) / 'go', target)
    print(f'Installed verified Go {version} for linux/{arch}')


if __name__ == '__main__':
    try:
        install(sys.argv[1])
    except Exception:
        sys.exit('Go download failed. Check network, official release availability and ~/.local/limmr-go; or set LIMMR_GO. No unverified toolchain was installed.')
