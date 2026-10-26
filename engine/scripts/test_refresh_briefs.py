#!/usr/bin/env python3
"""Offline safety tests: no HTTP, Codex, credentials or jobs outside temp fixtures."""
import datetime
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch, Mock
import importlib.util

spec = importlib.util.spec_from_file_location("refresh_briefs", Path(__file__).with_name("refresh-briefs.py"))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)

class NightlyTests(unittest.TestCase):
    def exercise(self, enabled):
        with tempfile.TemporaryDirectory(prefix="limmr-nightly-test-") as temporary:
            home = Path(temporary)
            (home / "control-token").write_text("invented-control-credential")
            calls = []
            permissions = {"read_messages": True, "read_people": True, "write_people": True, "save_summaries": True,
                           "chats": {"mode": "all", "jids": []}, "request_send": True, "download_media": True}
            class Clock(datetime.datetime):
                @classmethod
                def now(cls):
                    return cls(2026, 10, 17, 6, 0).astimezone()
            class Opener:
                def open(self, request, timeout):
                    path = request.full_url.split("18800")[-1]
                    calls.append((path, request.get_method(), request.data))
                    if path == "/v1/settings/briefs":
                        data = {"enabled": enabled, "time": "06:00"}
                    elif path == "/v1/refresh-plan":
                        data = {"circles": [{"circle_id": "atlas"}], "background": True}
                    elif path == "/v1/agents" and request.data is None:
                        data = {"agents": [{"id": "nightly", "name": "limmr-nightly", "transport": "local", "permissions": permissions}]}
                    elif path.endswith("/rotate"):
                        data = {"token": "invented-nightly-bearer"}
                    else:
                        data = {}
                    return io.BytesIO(json.dumps(data).encode())
            process = Mock(return_value=Mock(returncode=0))
            with patch.dict(os.environ, {"LIMMR_HOME": str(home)}, clear=False), \
                 patch.object(runner.urllib.request, "build_opener", return_value=Opener()), \
                 patch.object(runner.datetime, "datetime", Clock), \
                 patch.object(runner.subprocess, "run", process):
                runner.main()
                if not enabled:
                    process.assert_not_called()
                    self.assertEqual(len(calls), 1)
                    return
                process.assert_called_once()
                command = process.call_args.args[0]
                self.assertEqual(command[:2], ["codex", "exec"])
                self.assertIn("--ignore-user-config", command)
                self.assertIn("--ephemeral", command)
                self.assertNotIn("invented-nightly-bearer", " ".join(command))
                self.assertEqual(process.call_args.kwargs["env"]["LIMMR_NIGHTLY_TOKEN"], "invented-nightly-bearer")
                self.assertIn("Never send anything", process.call_args.kwargs["input"])
                grant = next(json.loads(data) for path, method, data in calls if method == "PATCH")
                self.assertTrue(grant["permissions"]["request_send"])  # owner may send Suggestions
                self.assertFalse(grant["permissions"]["download_media"])
                runner.main()
                process.assert_called_once()  # daily marker prevents double runs
    def test_disabled_never_invokes_codex(self):
        self.exercise(False)
    def test_enabled_uses_isolated_background_identity(self):
        self.exercise(True)

if __name__ == "__main__":
    unittest.main()
