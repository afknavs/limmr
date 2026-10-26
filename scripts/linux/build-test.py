#!/usr/bin/env python3
"""Offline argument, toolchain precedence and verified-download regressions."""
import hashlib
import importlib.util
import io
import json
import contextlib
import urllib.error
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = ROOT / 'scripts/linux/build.sh'
REQUIRED = next(line.split()[1] for line in (ROOT / 'engine/go.mod').read_text().splitlines() if line.startswith('go '))
spec = importlib.util.spec_from_file_location('download', SCRIPT.with_name('download-go.py'))
download = importlib.util.module_from_spec(spec)
spec.loader.exec_module(download)
spec = importlib.util.spec_from_file_location('smoke', SCRIPT.with_name('smoke.py'))
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class BuildTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home = Path(self.temp.name)
        self.bin = self.home / 'bin'
        self.bin.mkdir()
        for tool in ('dirname', 'awk', 'uname', 'cat'):
            (self.bin / tool).symlink_to(shutil.which(tool))
        self.env = dict(os.environ, HOME=str(self.home), PATH=str(self.bin), LIMMR_GO='', GOTOOLCHAIN='local')

    def go(self, path, version=REQUIRED, unrelated=False):
        path.parent.mkdir(parents=True, exist_ok=True)
        output = 'unrelated board game' if unrelated else f'go version go{version} linux/amd64'
        path.write_text(f'#!/bin/sh\n[ "$1" = version ] || exit 99\nprintf "%s\\n" "{output}"\n')
        path.chmod(0o700)
        return str(path)

    def run_script(self, *args, ok=True):
        result = subprocess.run(['/bin/sh', str(SCRIPT), *args], cwd=self.home, env=self.env,
                                stdin=subprocess.DEVNULL, text=True, capture_output=True)
        self.assertEqual(result.returncode == 0, ok, result.stderr)
        return result

    def test_help_and_bad_argument_do_not_need_go_or_network(self):
        self.assertIn('Usage:', self.run_script('--help').stdout)
        self.assertIn('Unknown argument', self.run_script('--typo', ok=False).stderr)
        self.assertIn('LIMMR_GO', self.run_script('--no-download', ok=False).stderr)

    def test_override_wins_and_handles_spaces(self):
        override = self.go(self.home / 'custom go')
        self.go(self.bin / 'go')
        self.env['LIMMR_GO'] = override
        self.assertEqual(self.run_script('--no-download', '--check-go').stdout.strip(), override)

    def test_unrelated_go_and_old_override_fall_back_to_private_go(self):
        self.env['LIMMR_GO'] = self.go(self.home / 'old', '1.20.0')
        self.go(self.bin / 'go', unrelated=True)
        private = self.go(self.home / '.local/limmr-go/bin/go')
        self.assertEqual(self.run_script('--no-download', '--check-go').stdout.strip(), private)

    def test_path_precedes_private_and_accepts_newer_version(self):
        selected = self.go(self.bin / 'go', '99.0.0')
        self.go(self.home / '.local/limmr-go/bin/go')
        self.assertEqual(self.run_script('--check-go', '--no-download').stdout.strip(), selected)

    def test_rejects_unrelated_old_prerelease_and_failing_programs(self):
        for version in ('1.0.0', '1.26rc1', '1.26.0.extra'):
            with self.subTest(version=version):
                self.go(self.bin / 'go', version)
                self.assertIn('Go >=', self.run_script('--no-download', '--check-go', ok=False).stderr)
        (self.bin / 'go').write_text('#!/bin/sh\nexit 1\n')
        self.run_script('--check-go', '--no-download', ok=False)

    def test_download_validates_checksum_before_installing(self):
        archive = io.BytesIO()
        with tarfile.open(fileobj=archive, mode='w:gz') as tar:
            content = b'fake compiler'
            member = tarfile.TarInfo('go/bin/go')
            member.size = len(content)
            member.mode = 0o755
            tar.addfile(member, io.BytesIO(content))
        data = archive.getvalue()
        for corrupt in (True, False):
            metadata = [{'version': f'go{REQUIRED}', 'files': [{
                'filename': f'go{REQUIRED}.linux-arm64.tar.gz',
                'sha256': '0' * 64 if corrupt else hashlib.sha256(data).hexdigest()}]}]
            calls = []

            def fetch(url, **kwargs):
                calls.append(url)
                return io.BytesIO(json.dumps(metadata).encode() if 'mode=json' in url else data)

            with patch.object(Path, 'home', return_value=self.home), patch.object(download.platform, 'machine', return_value='aarch64'), patch.object(download.urllib.request, 'urlopen', side_effect=fetch):
                if corrupt:
                    with self.assertRaisesRegex(ValueError, 'SHA-256 mismatch'):
                        download.install(REQUIRED)
                    self.assertFalse((self.home / '.local/limmr-go').exists())
                else:
                    download.install(REQUIRED)
                    self.assertEqual((self.home / '.local/limmr-go/bin/go').read_bytes(), b'fake compiler')
            self.assertEqual(len(calls), 2)
            self.assertTrue(all(url.startswith('https://go.dev/dl/') for url in calls))

    def test_smoke_waits_for_full_startup_and_cleans_up(self):
        homes, stopped, requests = [], [], []
        binary = self.home / 'engine'
        binary.write_bytes(b'fake binary')

        class Process:
            def __init__(self, args, **kwargs):
                home = Path(kwargs['env']['LIMMR_HOME'])
                homes.append(home)
                (home / 'control-token').write_text('synthetic-private-control')
            def poll(self): return None
            def send_signal(self, sig): stopped.append(sig)
            def wait(self, **kwargs): return 0

        class HTTP:
            attempts = 0
            def open(self, req, **kwargs):
                requests.append(req.full_url)
                if req.full_url.endswith('/v1/health'):
                    data = {'state': 'starting'}
                elif req.full_url.endswith('/v1/status'):
                    self.attempts += 1
                    if self.attempts == 1:
                        raise urllib.error.HTTPError(req.full_url, 503, 'starting', {}, None)
                    data = {'engine_version': 'test'}
                elif req.full_url.endswith('/v1/agents'):
                    data = {'token': 'synthetic-private-agent'}
                else:
                    body = json.loads(req.data)
                    data = {'jsonrpc': '2.0', 'result': {'protocolVersion': '2025-11-25'}
                            if body['method'] == 'initialize' else {'content': 'Linux smoke'}}
                return io.BytesIO(json.dumps(data).encode())

        output = io.StringIO()
        with patch.object(smoke.subprocess, 'Popen', Process), patch.object(smoke.urllib.request, 'build_opener', return_value=HTTP()), patch.object(smoke.time, 'sleep'), contextlib.redirect_stdout(output):
            smoke.smoke(str(binary))
        self.assertEqual(sum(url.endswith('/v1/status') for url in requests), 2)
        self.assertIn('Smoke test passed', output.getvalue())
        self.assertNotIn('synthetic-private', output.getvalue())
        self.assertTrue(stopped)
        self.assertTrue(all(not home.exists() for home in homes))


if __name__ == '__main__':
    unittest.main()
