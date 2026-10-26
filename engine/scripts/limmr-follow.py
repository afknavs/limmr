#!/usr/bin/env python3
"""Follow one AI's Limmr feed. Usage: limmr-follow.py /path/to/token-file."""
import argparse
import json
import time
import urllib.error
import urllib.request


def follow(token_file, url):
    cursor = ""
    backoff = 1
    while True:
        try:
            # Read on reconnect so the owner can rotate the credential in place.
            with open(token_file, encoding="utf-8") as source:
                token = source.read().strip()
            if not token or "\n" in token or "\r" in token:
                raise ValueError("invalid credential")
            headers = {"Authorization": "Bearer " + token, "Accept": "text/event-stream"}
            if cursor:
                headers["Last-Event-ID"] = cursor
            request = urllib.request.Request(url, headers=headers)
            # Redirects could leak the bearer credential to another host.
            class NoRedirect(urllib.request.HTTPRedirectHandler):
                def redirect_request(self, req, fp, code, msg, hdrs, newurl):
                    return None
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect)
            with opener.open(request, timeout=40) as response:
                data, next_cursor = [], ""
                for raw in response:
                    line = raw.decode("utf-8").rstrip("\r\n")
                    if not line:
                        if data:
                            item = json.loads("\n".join(data))
                            print(json.dumps(item, ensure_ascii=False), flush=True)
                        if next_cursor:
                            cursor = next_cursor
                        data, next_cursor = [], ""
                        backoff = 1
                    elif line.startswith("data:"):
                        data.append(line[5:].lstrip())
                    elif line.startswith("id:"):
                        next_cursor = line[3:].strip()
        except (OSError, ValueError, urllib.error.URLError):
            # Never print exceptions, headers, tokens, URLs or response bodies.
            pass
        time.sleep(backoff)
        backoff = min(backoff * 2, 30)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("token_file")
    args = parser.parse_args()
    try:
        follow(args.token_file, "http://127.0.0.1:18800/v1/updates/stream")
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
