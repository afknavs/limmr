#!/usr/bin/env python3
"""Offline follower checks: resumable SSE and credential-safe failures."""
import contextlib
import importlib.util
import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import urllib.error

spec = importlib.util.spec_from_file_location("limmr_follow", Path(__file__).with_name("limmr-follow.py"))
follower = importlib.util.module_from_spec(spec)
spec.loader.exec_module(follower)


class StopFollowing(Exception):
    pass


class FollowTests(unittest.TestCase):
    def test_resume_after_page_and_private_failure(self):
        with tempfile.TemporaryDirectory(prefix="limmr-follow-test-") as home:
            token = "invented-test-credential"
            path = Path(home) / "token"
            path.write_text(token)
            output, requests, delays = io.StringIO(), [], []

            class Opener:
                def open(self, request, timeout):
                    requests.append(request)
                    if len(requests) == 1:
                        return io.BytesIO(b': heartbeat\n\nid: page-cursor\nevent: update\ndata: {"message_id":"demo-one"}\n\n')
                    raise urllib.error.URLError(token)

            def backoff(delay):
                delays.append(delay)
                if len(delays) == 2:
                    raise StopFollowing()

            with patch.object(follower.urllib.request, "build_opener", return_value=Opener()) as build, patch.object(follower.time, "sleep", side_effect=backoff), contextlib.redirect_stdout(output):
                with self.assertRaises(StopFollowing):
                    follower.follow(str(path), "http://127.0.0.1:18800/v1/updates/stream")
            self.assertEqual(output.getvalue(), '{"message_id": "demo-one"}\n')
            self.assertEqual(requests[1].get_header("Last-event-id"), "page-cursor")
            self.assertEqual(requests[0].get_header("Authorization"), "Bearer " + token)
            self.assertEqual(delays, [1, 2])
            self.assertEqual(build.call_args.args[0].proxies, {})
            self.assertIsNone(build.call_args.args[1]().redirect_request(None, None, 302, "", {}, "https://example.invalid"))

    def test_missing_token_is_silent_and_backs_off(self):
        output, delays = io.StringIO(), []

        def backoff(delay):
            delays.append(delay)
            if len(delays) == 8:
                raise StopFollowing()

        with tempfile.TemporaryDirectory(prefix="limmr-follow-test-") as home, patch.object(follower.time, "sleep", side_effect=backoff), contextlib.redirect_stdout(output):
            with self.assertRaises(StopFollowing):
                follower.follow(str(Path(home) / "absent"), "http://127.0.0.1:18800/v1/updates/stream")
        self.assertEqual(output.getvalue(), "")
        self.assertEqual(delays, [1, 2, 4, 8, 16, 30, 30, 30])


if __name__ == "__main__":
    unittest.main()
