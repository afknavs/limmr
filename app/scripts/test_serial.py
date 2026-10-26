#!/usr/bin/env python3
"""Run Swift tests in a private home and report every Swift Testing issue.

The JSON event stream includes issues with no testID, which per-test/xUnit
summaries can omit. Preserve Swift's exit status and fail on missing diagnostics.
"""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def report_events(path, write=print):
    tests = {}
    failures = 0
    recorded = 0
    claimed = 0
    for line in Path(path).read_text().splitlines():
        record = json.loads(line)
        payload = record.get("payload", {})
        if record.get("kind") == "test":
            tests[payload["id"]] = payload
        if payload.get("kind") == "issueRecorded":
            recorded += 1
            issue = payload.get("issue", {})
            test = tests.get(payload.get("testID"), {})
            source = issue.get("sourceLocation") or test.get("sourceLocation") or {}
            location = f'{source.get("filePath", source.get("fileID", "<unavailable>"))}:{source.get("line", 0)}:{source.get("column", 0)}'
            messages = "; ".join(m.get("text", "") for m in payload.get("messages", []))
            write(f'A2 Swift issue: {location} [{payload.get("testID", "outside a test")}] {messages}')
            if issue.get("isFailure", True) and not issue.get("isKnown", False):
                failures += 1
            if not source:
                write("A2 Swift issue has no source location in the test runner event.")
                failures += 1
        if payload.get("kind") == "runEnded":
            for message in payload.get("messages", []):
                match = re.search(r"with (\d+) issues?", message.get("text", ""))
                if match:
                    claimed += int(match[1])
    if claimed > recorded:
        write(f"A2 Swift diagnostics incomplete: summaries report {claimed} issues but the stream contains {recorded}.")
        failures += 1
    write(f"A2 Swift event audit: {recorded} issues, {failures} unexpected failures.")
    return failures


def self_test():
    # Deliberately omit testID: an issue outside any test must still print.
    with tempfile.TemporaryDirectory(dir=ROOT / ".cache", prefix="a2-reporter-") as folder:
        path = Path(folder) / "events.jsonl"
        events = [{"kind": "event", "payload": {"kind": "issueRecorded", "issue": {
            "isFailure": True, "sourceLocation": {"filePath": "Fixture.swift", "line": i + 1, "column": 2}},
            "messages": [{"text": "synthetic issue"}]}} for i in range(8)]
        path.write_text("\n".join(map(json.dumps, events)))
        output = []
        assert report_events(path, output.append) == 8
        assert len([line for line in output if "outside a test" in line]) == 8
        assert all(f"Fixture.swift:{i + 1}:2" in output[i] for i in range(8))
        path.write_text(json.dumps({"kind": "event", "payload": {"kind": "runEnded", "messages": [{"text": "Test run failed with 8 issues."}]}}))
        assert report_events(path, lambda _: None) == 1
    print("A2 Swift reporter self-test passed (8 unassociated issues, locations, missing-event failure).")


def main():
    cache = ROOT / ".cache"
    cache.mkdir(exist_ok=True)
    if sys.argv[1:] == ["--self-test"]:
        self_test()
        return 0
    if sys.argv[1:]:
        raise SystemExit("Usage: python3 app/scripts/test_serial.py [--self-test]")
    with tempfile.TemporaryDirectory(dir=cache, prefix="a2-swift-home-") as home:
        env = dict(os.environ, LIMMR_HOME=home)
        stream = cache / "a2-swift-events.jsonl"
        stream.unlink(missing_ok=True)
        result = subprocess.run([
            "swift", "test", "--package-path", str(ROOT / "app"), "--disable-sandbox", "--no-parallel",
            "--cache-path", str(cache / "swiftpm"), "--config-path", str(cache / "swiftpm-config"),
            "--security-path", str(cache / "swiftpm-security"),
            "--event-stream-output-path", str(stream),
        ], env=env, stdin=subprocess.DEVNULL)
        if not stream.exists():
            print("A2 Swift diagnostics unavailable: no event stream was produced.", file=sys.stderr)
            return result.returncode or 1
        try:
            failures = report_events(stream)
        except (ValueError, OSError) as error:
            print(f"A2 Swift event stream could not be read: {type(error).__name__}.", file=sys.stderr)
            return result.returncode or 1
        return result.returncode or (1 if failures else 0)


if __name__ == "__main__":
    sys.exit(main())
