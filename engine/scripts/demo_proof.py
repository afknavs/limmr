#!/usr/bin/env python3
"""Run the built demo binary and curl its real HTTP/MCP surfaces; never print credentials."""
from datetime import datetime
import json
import os
from pathlib import Path
import subprocess
import socket
import tempfile
import time


def main():
    engine = Path(__file__).resolve().parents[1]
    binary = engine / "bin/limmr-engine"
    port = int(os.environ.get("LIMMR_PROOF_PORT", "19853"))
    if "LIMMR_PROOF_PORT" not in os.environ:
        try:
            with socket.socket() as probe:
                probe.bind(("127.0.0.1", 0))
                port = probe.getsockname()[1]
        except OSError:
            pass
    base = f"http://127.0.0.1:{port}"
    with tempfile.TemporaryDirectory(prefix="limmr-demo-proof-") as home:
        env = dict(os.environ, LIMMR_HOME=home)
        with tempfile.TemporaryFile() as output:
            process = subprocess.Popen(
                [str(binary), "--demo", "--home", home, "--port", str(port),
                 "--remote-port", str(port + 1)], env=env, stdin=subprocess.DEVNULL,
                stdout=output, stderr=output)
            try:
                def curl(path, credential, body=None, method=None, expected_status=None):
                    # Curl reads its private header from stdin, never command-line args.
                    config = f'header = "Authorization: Bearer {credential}"\n'
                    args = ["curl", "--silent", "--max-time", "3", "--config", "-",
                            "--header", "Content-Type: application/json",
                            "--header", "Accept: application/json, text/event-stream", base + path]
                    if expected_status is not None:
                        args += ["--write-out", "\n%{http_code}"]
                    else:
                        args += ["--fail"]
                    if method is not None:
                        args += ["--request", method]
                    if body is not None:
                        args += ["--data-binary", json.dumps(body)]
                    result = subprocess.run(args, input=config, text=True, capture_output=True)
                    if result.returncode:
                        raise RuntimeError("Demo HTTP probe failed (credential/output withheld).")
                    if expected_status is not None:
                        response, code = result.stdout.rsplit("\n", 1)
                        assert int(code) == expected_status
                        return json.loads(response) if response.strip() else None
                    return json.loads(result.stdout) if result.stdout.strip() else None

                control_path = Path(home) / "control-token"
                for _ in range(100):
                    if process.poll() is not None:
                        output.seek(0)
                        detail = output.read().decode("utf-8", errors="replace").strip()
                        known = "Limmr could not open its loopback listener in this environment."
                        raise RuntimeError(known if detail == known else "Demo binary exited before serving (private output withheld).")
                    if control_path.exists():
                        control = control_path.read_text().strip()
                        try:
                            status = curl("/v1/status", control)
                            break
                        except RuntimeError:
                            pass
                    time.sleep(0.05)
                else:
                    raise RuntimeError("Demo listener did not become ready.")
                assert status["linked"] and status["connected"] and status["pending_approvals"] == 0
                assert len(curl("/v1/people", control)["people"]) == 13
                assert len(curl("/v1/groups", control)["groups"]) == 13
                assert len(curl("/v1/chats", control)["chats"]) == 30
                checks = curl("/v1/health", control)["checks"]
                assert sum(c["state"] == "warn" for c in checks) == 1
                assert len(curl("/v1/graph?limit=7", control)["nodes"]) == 7
                agent = curl("/v1/agents", control,
                             {"name": "Demo proof AI", "kind": "codex", "transport": "local"})
                token, agent_id = agent["token"], agent["agent"]["id"]
                def rpc(method, params, call_id):
                    result = curl("/mcp", token, {"jsonrpc": "2.0", "id": call_id,
                                                 "method": method, "params": params})
                    assert "error" not in result
                    return result["result"]
                initialized = rpc("initialize", {"protocolVersion": "2025-11-25", "capabilities": {},
                                  "clientInfo": {"name": "Demo proof", "version": "1"}}, 1)
                assert initialized["serverInfo"]["name"] == "limmr"
                tools = rpc("tools/list", {}, 2)["tools"]
                assert len(tools) == 19
                assert len(json.dumps({"tools": tools}, ensure_ascii=False, separators=(",", ":")).encode()) < 18000
                assert not {"send_message", "list_messages", "create_watch"} & {t["name"] for t in tools}
                for tool in tools:
                    if tool["name"] in {"send"}:
                        properties = tool["inputSchema"]["properties"]
                        assert properties["owner_instruction"]["maxLength"] == 500
                        assert properties["queue_for_approval"]["type"] == "boolean"
                who = rpc("tools/call", {"name": "whoami", "arguments": {}}, 3)
                assert not who.get("isError") and who["structuredContent"]["data"]["agent"]["id"] == agent_id
                assert "setup" in who["structuredContent"]["data"]
                index = rpc("tools/call", {"name": "more", "arguments": {}}, 310)
                assert len(index["structuredContent"]["data"]["actions"]) == 60
                aliases = rpc("tools/call", {"name": "more", "arguments": {"action": "list_chats", "args": {"limit": 1}}}, 311)
                assert not aliases.get("isError")
                found = rpc("tools/call", {"name": "find", "arguments": {"query": "Atlas", "types": ["circle"]}}, 312)
                assert not found.get("isError") and found["structuredContent"]["data"]["candidates"]
                digest = rpc("tools/call", {"name": "get_digest", "arguments": {"period": "today"}}, 300)
                assert not digest.get("isError")
                digest_data = digest["structuredContent"]["data"]
                assert {"waiting_on_you", "due", "circle_updates", "agent_work", "sends", "cursor"} == set(digest_data)
                route_digest = curl("/v1/digest?period=today", token)
                assert digest_data.pop("cursor") and route_digest.pop("cursor")
                assert digest_data == route_digest
                efforts = curl("/v1/efforts", control)["efforts"]
                assert {"Pitch deck with Maya", "Lab report", "Weekend plan", "Garden supplies", "Design Club poster"} <= {e["title"] for e in efforts}
                purpose = rpc("tools/call", {"name": "more", "arguments": {"action": "efforts.list", "args": {"q": "Pitch deck"}}}, 313)
                assert not purpose.get("isError") and purpose["structuredContent"]["data"]
                assert len(curl("/v1/efforts/" + efforts[0]["id"], control)["timeline"]) <= 50
                home_state = curl("/v1/home", control)
                assert "circle_updates" in home_state
                assert len(home_state["today"]) == 7
                assert len(home_state["suggestions"]) == 3 and len(home_state["proposals"]) == 4
                assert len(home_state["integrations"]) == 6
                assert len(home_state["queued"]) == 3 and len(home_state["needs_reply"]) == 3
                assert home_state["cards"] == home_state["today"]
                assert len(home_state["recent_activity"]) == 6
                assert all(2 <= len(s["variations"]) <= 3 and s["body"] == s["variations"][0] for s in home_state["queued"])
                family = next(s for s in home_state["queued"] if s["circle_id"] == "family")
                university = next(s for s in home_state["queued"] if s["circle_id"] == "university")
                assert all("signature" not in s and "voice" not in s and s["body"] == s["draft_text"] for s in home_state["queued"])
                cards = home_state["circles"]
                assert len(cards) == 7 and {c["circle"]["id"] for c in cards[:2]} == {"university", "atlas"}
                assert all(c["pinned"] and c["why_shown"] == "pinned" for c in cards[:2])
                assert any(c["circle"]["id"] == "flatmates" and c["why_shown"] == "open_ask" for c in cards)
                pinned = curl("/v1/circles/family", control, {"pinned": True}, method="PATCH")
                assert pinned["pinned"]
                curl("/v1/circles/family", control, {"pinned": False}, method="PATCH")
                ask = next(a for a in home_state["needs_reply"] if a["circle_id"] == "flatmates")
                assert all(a["id"] and a["chat_jid"] and a["chat"] and a["from"] and a["at"] for a in home_state["needs_reply"])
                assert curl("/v1/circles/flatmates/asks/" + ask["id"] + "/done", control, {}) is None
                assert all(a["id"] != ask["id"] for a in curl("/v1/home", control)["needs_reply"])
                sent = curl("/v1/suggestions/" + family["id"] + "/send", control,
                            {"text": "Owner edited train update.", "voice": "as_me", "variation": 1})
                assert sent["final_text"] == "Owner edited train update." and "voice" not in sent
                assert sent["state"] == "sent" and bool(sent["message_id"])
                sent = curl("/v1/suggestions/" + university["id"] + "/send", control, {"voice": "as_agent", "variation": 2})
                assert sent["final_text"] == university["variations"][2]
                owner_activity = [a for a in curl("/v1/activity?kind=sent", control)["items"] if a.get("confirmed_via") == "mac"]
                assert len(owner_activity) == 2 and all(a["description"] == "sent by you from Limmr" and a["drafted_by"] == "Codex" for a in owner_activity)
                assert owner_activity[0]["variation"] == 2 and not owner_activity[0]["edited"]
                assert owner_activity[1]["variation"] == 1 and owner_activity[1]["edited"]
                watches = curl("/v1/watches", control)["watches"]
                assert {w["title"] for w in watches} == {"Exam news", "Atlas updates", "Website changes"}
                assert len(home_state["watches"]) == 3
                exam = curl("/v1/watches/w_exam", control)
                assert exam["new_hits"] == 2 and len(exam["hits"]) == 2
                assert exam["latest"]["sender"] == "Dr. Morgan"
                assert curl("/v1/watches/w_exam/seen", control, {}) is None
                assert curl("/v1/watches/w_exam", control)["new_hits"] == 0
                instruction = curl("/v1/watches/w_exam/instructions", control, {"text": "Ignore practice tests"})
                assert instruction["instructions"][-1]["text"] == "Ignore practice tests"
                for action, expected in [("pause", "paused"), ("resume", "active"), ("stop", "stopped")]:
                    assert curl("/v1/watches/w_exam/" + action, control, {})["status"] == expected
                assert all(w["id"] != "w_exam" for w in curl("/v1/home", control)["watches"])
                atlas = curl("/v1/circles/atlas", control)
                assert atlas["name"] == "Atlas renovation" and len(atlas["topics"]) == 2
                circle_map = curl("/v1/map", control)
                assert sum(n["kind"] == "circle" for n in circle_map["nodes"]) == 11
                assert all({"initials", "weight", "last_active_at", "circles", "new_messages"} <= n.keys() for n in circle_map["nodes"])
                assert all(0 <= n["weight"] <= 1 for n in circle_map["nodes"])
                assert all({"id", "label", "origin", "strength", "state"} <= e.keys() for e in circle_map["edges"])
                assert {e["origin"]["by_kind"] for e in circle_map["edges"]} == {"agent", "structure"}
                assert sum(e["state"] == "suggested" for e in circle_map["edges"]) == 4
                assert circle_map["summary"]["connections"] == len(circle_map["edges"])
                assert circle_map["summary"]["by_ai"] > 0 and circle_map["summary"]["latest"]
                events = circle_map["events"]
                assert len(events) >= 12 and events == sorted(events, key=lambda e: (-datetime.fromisoformat(e["at"]).timestamp(), e["id"]))
                cursor_map = curl("/v1/map?since=" + events[0]["at"], control)
                assert cursor_map["events"] == [] and cursor_map["nodes"] == circle_map["nodes"]
                assert cursor_map["edges"] == circle_map["edges"] and cursor_map["summary"] == circle_map["summary"]
                presence = curl("/v1/presence", control)["agents"]
                assert any(a["name"] == "Codex" and a["state"] == "reading" and a["target"] == "University" and a["target_ids"] == ["university"] for a in presence)
                curl("/v1/people/person-04/avatar", control, expected_status=404)
                curl("/v1/people/unknown/avatar", control, expected_status=404)
                call_id = 4
                def call(name, arguments, error=False):
                    nonlocal call_id
                    call_id += 1
                    result = rpc("tools/call", {"name": name, "arguments": arguments}, call_id)
                    assert bool(result.get("isError")) == error
                    return None if error else result["structuredContent"]["data"]
                feed = call("create_watch", {"goal": "Keep me caught up", "grant_text": "Keep me caught up on everything I can read",
                    "scope": {"all": True}, "match": {"kind": "any"}, "action": "feed", "title": "My updates"})
                assert feed["kind"] == "feed"
                waiting = curl("/v1/waiting", control)["waiting"]
                own_waiting = next(w for w in waiting if w["id"] == feed["id"])
                assert {"id", "agent", "kind", "sentence", "where", "since", "new_hits", "latest"} == own_waiting.keys()
                assert curl("/v1/home", control)["waiting"] == waiting
                updates = call("get_updates", {"wait_seconds": 0, "limit": 100})
                assert updates["cursor"] and updates["items"] == [] and not updates["more"]
                assert call("stop_watch", {"id": feed["id"]})["status"] == "stopped"
                created_watch = call("create_watch", {"goal": "Watch exam dates", "grant_text": "Watch exam dates",
                    "scope": {"circles": ["university"]}, "match": {"kind": "keywords", "keywords": ["exam"]},
                    "action": "notify", "title": "Proof watch"})
                assert created_watch["status"] == "active"
                changed_watch = call("update_watch", {"id": created_watch["id"], "instruction": "Ignore practice exams",
                    "digest": "No new dates.", "status": "paused"})
                assert changed_watch["digest"] == "No new dates." and len(changed_watch["instructions"]) == 1
                assert call("stop_watch", {"id": created_watch["id"]})["status"] == "stopped"
                handoff = call("start_handoff", {"goal": "Help Robin refine the website", "grant_text": "Help Robin refine the website",
                    "scope": {"chats": ["15550100012@s.whatsapp.net"]}, "chat_jid": "15550100012@s.whatsapp.net",
                    "runner": {"kind": "codex", "session_id": "proof-session", "workdir": home}})
                assert handoff["kind"] == "handoff" and handoff["expires_at"] is not None
                assert call("stop_handoff", {"id": handoff["id"]})["status"] == "stopped"
                resolved = call("resolve_context", {"query": "Bathroom tiles"})
                assert resolved["candidates"][0]["id"] == "atlas-tiles"
                assert not resolved["needs_clarification"]
                brief = call("get_brief", {"id": "atlas"})
                assert brief["brief"]["summary"] and len(brief["chats"]) == 2
                next_page = ""
                evidence = []
                while True:
                    page = call("catch_up", {"id": "atlas", "limit": 2, "page_token": next_page})
                    evidence += [m["message"]["id"] for g in page["chats"] for m in g["messages"]]
                    next_page = page.get("next_page_token", "")
                    if not next_page:
                        break
                assert len(evidence) == 6 and len(set(evidence)) == 6
                updated = call("update_brief", {"id": "atlas", "summary": "Robin wants darker bathroom tiles.",
                    "what_changed": "Tile samples are ready for review.", "open_asks": ["Choose a sample."],
                    "upcoming": [{"date": "2026-10-21", "text": "Site visit"}],
                    "people_notes": [{"person_id": "person-12", "contribution": "Requested darker tiles."}],
                    "evidence_message_ids": evidence})
                assert updated["version"] == 2
                suggestion = call("suggest", {"circle_id": "atlas", "kind": "reply", "chat_jid": "15550100012@s.whatsapp.net",
                    "draft_text": "The darker samples are ready.", "variations": ["The darker samples are ready.", "I have the samples ready."], "why": "Client requested a comparison."})
                assert suggestion["state"] == "pending"
                draft = call("draft_message", {"chat": "15550100007@s.whatsapp.net", "text": "Draft for review.", "voice": "as_agent", "variations": ["Draft for review.", "Please review this draft."]})
                assert draft["rule"] == "confirm" and draft["state"] == "draft"
                call("send_draft", {"draft_id": draft["draft_id"]}, error=True)
                instructed = call("send_message", {"recipient": "15550100007@s.whatsapp.net", "text": "The outline is ready.", "owner_instruction": "Send the outline to Maya now."})
                assert instructed["state"] == "sent" and instructed["confirmed_via"] == "owner_instruction"
                assert instructed["owner_instruction"] == "Send the outline to Maya now."
                consent = call("send_draft", {"draft_id": draft["draft_id"], "owner_instruction": "Yes, send that draft."})
                assert consent["state"] == "sent" and consent["confirmed_via"] == "owner_instruction"
                queued = call("send_message", {"recipient": "15550100007@s.whatsapp.net", "text": "Review this outline.", "owner_instruction": "Prepare this for review.", "queue_for_approval": True})
                assert queued["state"] == "suggested" and queued["queue_for_approval"]
                queued_home = curl("/v1/home", control)["queued"]
                assert any(item.get("draft_id") == queued["draft_id"] for item in queued_home)
                curl("/v1/suggestions/" + queued["suggestion_id"] + "/dismiss", control, {}, method="POST")
                audit = curl("/v1/activity?kind=sent", control)["items"]
                assert any(item.get("owner_instruction") == "Send the outline to Maya now." for item in audit)
                print("A1 proof PASS: owner-instructed confirm send, conversation confirmation, opt-in Home review and quoted audit; fake Messenger only.")
                free = call("draft_message", {"chat": "15550100012@s.whatsapp.net", "text": "Invented demo only.", "voice": "as_agent", "signed": False, "send_now": True})
                assert free["state"] == "sent" and free["confirmed_via"] == "free"
                assert free["message_id"]  # L1 pre-assigns the WhatsApp id so sends can be attributed
                assert free["final_text"] == "Invented demo only." and "signed" not in free
                changes = call("get_updates", {"cursor": updates["cursor"]})
                assert any(item["kind"] == "send_status" and item["send_id"] == free["draft_id"] and item["status"] == "sent" for item in changes["items"])
                print("W1 proof PASS: standing feed grant, waiting/Home contract, cursor catch-up and exact fake delivery with retained AI attribution.")
                print("Home v2 proof PASS: queued bodies/variations, pinned/live circles, 3 asks, Done, recent activity; owner edits and selected variations delivered by fake Messenger only.")
                print("Watches proof PASS: list/detail/seen/instructions/pause/resume/stop, Home filtering, MCP create/update/stop and handoff grant; demo fake runner only.")
                setup = call("get_started", {})
                assert setup["stage"] == "sorting" and setup["linked"]
                assert curl("/v1/setup/status", control)["stage"] == "sorting"
                proposals = curl("/v1/home", control)["proposals"]
                assert len(proposals) == 4 and len({p["batch_id"] for p in proposals}) == 1
                assert all(p["created_by"] == "agent-3" for p in proposals)
                agents = curl("/v1/agents", control)["agents"]
                acme = next(a for a in agents if a["name"] == "Acme Assistant")
                assert acme["access"]["circles"] == ["work"] and not acme["access"]["unsorted"]
                assert next(a for a in agents if a["id"] == "ended-assistant")["access_summary"].startswith("Ended ")
                curl("/v1/agents/" + agent_id + "/access", control,
                     {"circles": ["work"], "unsorted": False}, method="PATCH")
                assert {c["jid"] for c in call("list_chats", {})} == {"120363000006@g.us", "15550100009@s.whatsapp.net", "120363000007@g.us"}
                assert all(p["id"] not in ["person-02", "person-03"] for p in call("find_person", {"query": ""}))
                call("get_brief", {"id": "family"}, error=True)
                assert all(c["id"] != "family" for c in call("resolve_context", {"query": "family"})["candidates"])
                curl("/v1/agents/" + agent_id + "/access", control, {"until": "2026-10-16T00:00:00Z"}, method="PATCH")
                call("list_chats", {}, error=True)
                assert call("whoami", {})["access"]["ended"]
                assert len(curl("/v1/activity?kind=access_ended", control)["items"]) == 1
                curl("/v1/agents/" + agent_id + "/access", control, {"until": None}, method="PATCH")
                assert call("list_chats", {})
                built_in = curl("/v1/agents/this-mac/enable", control, {}, method="POST")
                assert built_in["id"] == "this-mac" and "token" not in built_in
                brief = curl("/v1/circles/work/brief", control,
                             {"summary": "Work draft reviewed on this Mac.", "what_changed": "On-device draft", "written_by": "this-mac"}, method="PUT")
                assert brief["written_by"] == "this-mac" and brief["version"] == 2
                curl("/v1/suggestions", control, {"circle_id": "work", "kind": "action", "why": "Review the work brief", "created_by": "this-mac"})
                reviewed = curl("/v1/circles/proposals/accept", control, {"ids": [proposals[0]["id"]]})
                assert reviewed["circles"][0]["confirmed"]
                curl("/v1/circles/proposals/dismiss", control, {"ids": [p["id"] for p in proposals[1:]]})
                assert curl("/v1/home", control)["proposals"] == []
                print("Round 10 proof PASS: scoped access, expiry/renewal, setup, proposal batch/bulk review and attributed This Mac writes.")
                print("Understanding proof PASS: attributed map, four pending edges, two-week events, since cursor, summary, Codex presence and demo avatar 404.")
                print("Demo HTTP proof PASS: 7 circles, 3 suggestions, 2 proposals, 13 people, 13 groups, 30 chats; Home + Map + circle detail.")
                print("MCP proof PASS: initialize, 19 tools with hidden aliases, identity, digest, resolve, brief, paged catch-up (6), update, suggest, guarded confirmation, fake free delivery.")

            finally:
                if process.poll() is None:
                    process.terminate()
                    try:
                        process.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=5)
                print("Demo process stopped; throwaway home removed on exit.")


if __name__ == "__main__":
    try:
        main()
    except (RuntimeError, AssertionError) as error:
        raise SystemExit(str(error) or "Demo response assertion failed (output withheld).")
