#!/usr/bin/env python3
"""Only invented data, private credentials in memory, random loopback listeners."""
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request


def smoke(binary):
    with tempfile.TemporaryDirectory(prefix='limmr-smoke-') as home:
        sockets = [socket.socket(), socket.socket()]
        try:
            for sock in sockets:
                sock.bind(('127.0.0.1', 0))
            ports = [sock.getsockname()[1] for sock in sockets]
        finally:
            for sock in sockets:
                sock.close()
        env = dict(os.environ, LIMMR_HOME=home)
        proc = subprocess.Popen([binary, '--demo', '--home', home, '--port', str(ports[0]),
                                 '--remote-port', str(ports[1])], env=env, stdin=subprocess.DEVNULL,
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        # Do not honor machine HTTP proxies for this private loopback test.
        http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

        def request(path, token, body=None):
            headers = {'Authorization': 'Bearer ' + token, 'Accept': 'application/json, text/event-stream'}
            data = None
            if body is not None:
                headers['Content-Type'] = 'application/json'
                data = json.dumps(body).encode()
            req = urllib.request.Request(f'http://127.0.0.1:{ports[0]}' + path, data=data, headers=headers)
            with http.open(req, timeout=3) as response:
                return json.load(response)

        try:
            deadline = time.monotonic() + 60
            while True:
                if proc.poll() is not None:
                    raise RuntimeError('Engine exited before health readiness')
                try:
                    control = (Path(home) / 'control-token').read_text().strip()
                    request('/v1/health', control)
                    # Startup health responds before the full API is ready.
                    status = request('/v1/status', control)
                    break
                except (OSError, ValueError) as error:
                    if hasattr(error, "close"):
                        error.close()
                    if time.monotonic() >= deadline:
                        raise RuntimeError('Engine health deadline exceeded') from None
                    time.sleep(0.1)
            agent = request('/v1/agents', control, {'name': 'Linux smoke', 'kind': 'custom', 'transport': 'local'})
            token = agent['token']

            def rpc(ident, method, params):
                result = request('/mcp', token, {'jsonrpc': '2.0', 'id': ident, 'method': method, 'params': params})
                if 'error' in result or 'result' not in result or result['result'].get('isError'):
                    raise RuntimeError('MCP check failed')
                return result['result']

            initialized = rpc(1, 'initialize', {'protocolVersion': '2025-11-25', 'capabilities': {},
                                              'clientInfo': {'name': 'linux-smoke', 'version': '1'}})
            if 'protocolVersion' not in initialized:
                raise RuntimeError('Missing MCP protocol version')
            identity = rpc(2, 'tools/call', {'name': 'whoami', 'arguments': {}})
            if 'Linux smoke' not in json.dumps(identity):
                raise RuntimeError('whoami did not identify the fresh local agent')
            print('Smoke test passed')
            print('Limmr engine version: ' + status['engine_version'])
            digest = hashlib.sha256()
            with open(binary, 'rb') as source:
                for block in iter(lambda: source.read(1024 * 1024), b''):
                    digest.update(block)
            print('SHA-256: ' + digest.hexdigest())
        finally:
            if proc.poll() is None:
                proc.send_signal(signal.SIGINT)
                try:
                    proc.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()


if __name__ == '__main__':
    try:
        smoke(str(Path(sys.argv[1]).resolve()))
    except Exception:
        # Exception strings or engine responses could contain a credential.
        sys.exit('Smoke test failed (health, initialize or whoami); temporary engine stopped and home removed. No credentials printed.')
