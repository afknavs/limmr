import Foundation
import Testing

@testable import LimmrKit

@Suite("U3 activity presentation") struct U3PresentationTests {
  private func row(
    _ id: Int64, _ at: String, agent: String = "a", kind: String = "read", count: Int = 1,
    chat: String = "one", tool: String = "list_messages"
  ) -> Activity {
    .init(
      id: id, at: at, agentId: agent, tool: tool, kind: kind, chatJids: [chat], messageIds: [],
      count: count)
  }
  @Test func groupingIsOrderIndependentAndSplitsAtInactivityDayAndAgent() {
    let rows = [
      row(1, "2026-10-17T10:00:00Z", count: 8),
      row(2, "2026-10-17T10:30:00Z", count: 6, chat: "two"),
      row(3, "2026-10-17T11:00:01Z"),
      row(4, "2026-10-17T10:15:00Z", agent: "b"),
      row(5, "2026-10-17T23:59:00Z"), row(6, "2026-10-18T00:00:00Z"),
    ]
    let sessions = ActivityPresentation.sessions(rows)
    #expect(sessions == ActivityPresentation.sessions(rows.reversed()))
    #expect(sessions.count == 5)
    let merged = sessions.first { $0.activityIds == [1, 2] }
    #expect(merged?.count == 14 && merged?.chatJids == ["one", "two"])
    #expect(
      merged?.text(
        agent: "Codex", chatName: { $0 == "one" ? "Design Club" : "Weekend Walks" },
        circleName: { $0 }) == "Codex caught up on Design Club and Weekend Walks · 14 messages")
  }
  @Test func groupingUsesInstantsWithFractionalSecondsAndOffsets() {
    let rows = [
      row(1, "2026-10-17T10:00:00Z"), row(2, "2026-10-17T10:00:00.500Z"),
      row(3, "2026-10-17T16:01:00+05:30"),
    ]
    let sessions = ActivityPresentation.sessions(rows)
    #expect(sessions.count == 2)
    #expect(sessions.last?.activityIds == [1, 2])
    #expect(sessions.last?.end == "2026-10-17T10:00:00.500Z")
  }
  @Test func emptyReadsAndDirectoryCountsNeverBecomeMessageReads() {
    let rows = [
      row(1, "2026-10-17T10:00:00Z", count: 0),
      row(2, "2026-10-17T10:01:00Z", count: 200, tool: "list_chats"),
      row(3, "2026-10-17T10:02:00Z", kind: "people_read", count: 0, tool: "get_person"),
    ]
    #expect(ActivityPresentation.sessions(rows).isEmpty)
    #expect(ActivityPresentation.visible(rows).isEmpty)
  }
  @Test func compactToolsKeepMessageReadsAndExcludeDirectoryLookups() {
    let metadata = row(1, "2026-10-17T10:00:00Z", count: 200, tool: "read")
    let directory = row(2, "2026-10-17T10:01:00Z", kind: "search", count: 40, tool: "find")
    var messages = row(3, "2026-10-17T10:02:00Z", count: 2, tool: "read")
    messages.messageIds = ["one", "two"]
    var search = row(4, "2026-10-17T10:03:00Z", kind: "search", count: 1, tool: "find")
    search.messageIds = ["three"]
    let sessions = ActivityPresentation.sessions([metadata, directory, messages, search])
    #expect(sessions.count == 1)
    #expect(sessions.first?.activityIds == [3, 4])
    #expect(sessions.first?.count == 3)
  }
  @Test func sendsFirstAndDefaultFilterContainsDraftsButNoReads() {
    let read = row(1, "2026-10-17T11:00:00Z")
    let note = row(2, "2026-10-17T12:00:00Z", kind: "summary", tool: "save_summary")
    let sent = row(3, "2026-10-17T10:00:00Z", kind: "sent", tool: "send_draft")
    let draft = row(4, "2026-10-17T09:00:00Z", kind: "suggestion", tool: "draft_message")
    #expect(ActivityPresentation.visible([note, read, draft, sent]).map(\.id) == [3, 4, 2])
    #expect(
      ActivityPresentation.visible([note, read, draft, sent], filter: "sends").map(\.id) == [3, 4])
  }
  @Test func statsReceiptAndDigestContractsDecode() throws {
    let data = Data(
      #"{"today":{"messages":3,"sends":2,"as_me":1,"as_agent":1,"unknown_voice":0,"read_sessions":1},"week":{"messages":15,"sends":2,"as_me":1,"as_agent":1,"unknown_voice":0,"read_sessions":4},"agents":[{"id":"a","today":1,"week":4}],"recent_sends":[{"id":3,"at":"2026-10-17T10:00:00Z","agent_id":"a","tool":"send_draft","kind":"sent","chat_jids":["one"],"message_ids":[],"count":1,"voice":"as_me","status":"read","note":"The outline is ready."}],"last_sync_at":"2026-10-17T12:00:00Z","tracking_since":"2026-10-12T00:00:00Z"}"#
        .utf8)
    let stats = try Wire.decode(DashboardStats.self, from: data)
    #expect(stats.week.readSessions == 4 && stats.today.messages == 3)
    #expect(ActivityPresentation.sendStatus(stats.recentSends[0]) == "Read")
    #expect(stats.today.sends == 2 && stats.recentSends[0].agentId == "a")
    let roundTrip = try Wire.decode(DashboardStats.self, from: Wire.data(stats))
    #expect(roundTrip.recentSends[0].note == "The outline is ready.")
    let digests = try Wire.decode(
      ActivityDigests.self, from: Wire.data(DemoData.digests(activity: DemoData.activity)))
    #expect(digests.digests.allSatisfy { $0.messages > 0 && !$0.text.contains("0 messages") })
    #expect(digests.digests == DemoData.digests(activity: DemoData.activity.reversed()).digests)
  }
}
