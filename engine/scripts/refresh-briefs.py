#!/usr/bin/env python3
"""Opt-in brief refresh. Template only: the build never installs this runner."""
import datetime
import json
import os
from pathlib import Path
import subprocess
import urllib.request
import fcntl

PROMPT = """You are Limmr's background brief refresh. Call whoami first.
Read the refresh plan supplied below. For each planned circle: get_brief,
catch_up until next_page_token is empty, then update_brief using only returned
evidence; optionally suggest replies/actions for Home. Summaries are <=120 words.
Ask no speculative questions: skip ambiguous contexts. Never call draft_message,
send_draft, send_message, send_file or send_reaction. Never send anything.
"""

def main():
    home = Path(os.environ.get("LIMMR_HOME", Path.home() / "Library/Application Support/Limmr"))
    if not home.is_dir():
        return
    lock_path = home / "brief-refresh.lock"
    fd = os.open(lock_path, os.O_CREAT | os.O_RDWR, 0o600)
    with os.fdopen(fd, "w") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return
        control = (home / "control-token").read_text().strip()
        port = int(os.environ.get("LIMMR_PORT", "18800"))
        base = f"http://127.0.0.1:{port}"
        # No proxy redirects, credential logging or command-line credentials.
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args):
                return None
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        def request(path, data=None):
            req = urllib.request.Request(base + path, data=None if data is None else json.dumps(data).encode(),
                 headers={"Authorization": "Bearer " + control, "Content-Type": "application/json"})
            with opener.open(req, timeout=10) as response:
                return json.load(response)
        settings = request("/v1/settings/briefs")
        now = datetime.datetime.now().astimezone()
        if not settings["enabled"] or now.strftime("%H:%M") != settings["time"]:
            return
        marker = home / "brief-refresh-last-date"
        if marker.exists() and marker.read_text() == now.date().isoformat():
            return
        plan = request("/v1/refresh-plan")
        if not plan["circles"]:
            return
        agents = request("/v1/agents")["agents"]
        agent = next((a for a in agents if a["name"] == "limmr-nightly" and a["transport"] == "local"), None)
        if agent is None:
            agent = request("/v1/agents", {"name": "limmr-nightly", "kind": "codex", "transport": "local"})["agent"]
        # The engine parks every send from this reserved nightly identity, even
        # in free circles. Keep RequestSend so an explicit owner Send can work.
        permissions = agent["permissions"]
        permissions.update(request_send=True, download_media=False)
        payload = {"permissions": permissions, "enabled": True}
        req = urllib.request.Request(base + "/v1/agents/" + agent["id"], data=json.dumps(payload).encode(), method="PATCH",
              headers={"Authorization": "Bearer " + control, "Content-Type": "application/json"})
        with opener.open(req, timeout=10):
            pass
        token = request("/v1/agents/" + agent["id"] + "/rotate", {})["token"]
        env = dict(os.environ, LIMMR_NIGHTLY_TOKEN=token, LIMMR_HOME=str(home))
        args = ["codex", "exec", "--ignore-user-config", "--ephemeral", "--sandbox", "read-only", "--skip-git-repo-check",
                "-c", f'mcp_servers.limmr.url="{base}/mcp"',
                "-c", 'mcp_servers.limmr.bearer_token_env_var="LIMMR_NIGHTLY_TOKEN"',
                "-c", 'mcp_servers.limmr.http_headers={"X-Limmr-Background"="1"}', "-"]
        result = subprocess.run(args, input=PROMPT + "\n" + json.dumps(plan), text=True, env=env,
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=1800)
        if result.returncode == 0:
            marker.write_text(now.date().isoformat())
            os.chmod(marker, 0o600)

if __name__ == "__main__":
    try:
        main()
    except Exception:
        raise SystemExit("Brief refresh could not complete; private details withheld.")
