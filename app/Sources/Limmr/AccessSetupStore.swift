import LimmrKit
import SwiftUI

extension AppStore {
  static let starterPrompt = "Set up Limmr for me."
  func switchPreset(_ preset: String, agentID: String) async {
    let source = backend
    let generation = backendGeneration
    // Finish older capability saves before resetting the grants to a preset.
    await waitForPermissionSave()
    guard generation == backendGeneration else { return }
    await perform(action: "permissions") {
      try await source.post("/v1/agents/\(agentID)/preset", ["preset": preset])
      guard generation == self.backendGeneration else { return }
      await self.refresh()
      await self.loadPermissions()
    }
  }
  func accessSummary(_ access: AgentAccess) -> String {
    access.summary(
      names: Dictionary(uniqueKeysWithValues: circles.map { ($0.id, $0.name) }),
      at: demo ? Timestamp.date(DemoData.now)! : .now)
  }
  func refreshSetup() async {
    let generation = backendGeneration
    let source = backend
    async let setup = try? source.setupStatus()
    async let briefs = try? source.get("/v1/settings/briefs", as: BriefSettings.self)
    let values = await (setup, briefs)
    guard generation == backendGeneration else { return }
    setupStatus = values.0
    briefSettings = values.1 ?? BriefSettings()
  }
  func reviewProposals(_ ids: [String], keep: Bool) async {
    await perform(action: "proposal-review") {
      try await self.backend.reviewProposals(ids, keep: keep)
      await self.refresh()
    }
    if keep && error == nil && briefSettings.thisMacEnabled { await writeMacBriefs() }
  }
  func enableMacBriefs(_ enabled: Bool) async {
    let source = backend
    let generation = backendGeneration
    await perform(action: "mac-briefs") {
      if enabled && !self.demo, let reason = AppleBriefModel.unavailableReason {
        throw BackendError.unavailable(reason)
      }
      let settings = try await source.setThisMacBriefs(enabled)
      guard generation == self.backendGeneration else { return }
      self.briefSettings = settings
    }
    if enabled && briefSettings.thisMacEnabled { await writeMacBriefs() }
  }
  func writeMacBriefs() async {
    guard !writingMacBriefs, briefSettings.thisMacEnabled else { return }
    if demo || snapshot {
      macBriefNotice = "Sample mode · No on-device model was run."
      return
    }
    writingMacBriefs = true
    defer { writingMacBriefs = false }
    let source = backend
    let generation = backendGeneration
    await perform(action: "mac-briefs") {
      let writer = OnDeviceBriefWriter(
        backend: source, model: AppleBriefModel(), currentDays: self.settings.currentDays,
        now: self.currentNow)
      let list: CirclesResponse = try await source.get("/v1/circles")
      for circle in list.circles where circle.confirmed {
        guard generation == self.backendGeneration, self.briefSettings.thisMacEnabled else {
          return
        }
        // This Mac supplies first briefs; a connected AI's understanding is left intact.
        let history: BriefsResponse = try await source.get("/v1/circles/\(circle.id)/briefs")
        if history.briefs.isEmpty { try await writer.write(circleID: circle.id) }
      }
      guard generation == self.backendGeneration else { return }
      self.macBriefNotice = "First briefs are up to date."
      await self.refresh()
    }
  }
}
