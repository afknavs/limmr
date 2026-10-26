import LimmrKit
import SwiftUI

struct SetupHomeCard: View {
  @Bindable var store: AppStore
  let setup: SetupStatus
  @AppStorage("homeSetupDismissedReason") private var dismissedReason = ""

  // Proposal batches give later reviews their own reason after Not now.
  static func reason(stage: String, proposals: [CircleProposal], profile: OwnerProfile) -> String {
    if !proposals.isEmpty {
      return "proposals:" + Set(proposals.map { $0.batchId ?? $0.id }).sorted().joined(separator: ",")
    }
    if stage == "new" { return "new" }
    if profile.name.isEmpty && profile.about.isEmpty && profile.studies.isEmpty
      && profile.work.isEmpty && profile.people.isEmpty && profile.projects.isEmpty {
      return "about-you-empty"
    }
    return ""
  }
  static func shouldShow(reason: String, dismissedReason: String) -> Bool {
    !reason.isEmpty && reason != dismissedReason
  }
  private var reason: String {
    Self.reason(stage: setup.stage, proposals: store.home.proposals, profile: store.ownerProfile)
  }
  var body: some View {
    if Self.shouldShow(reason: reason, dismissedReason: dismissedReason) {
      HStack(alignment: .center, spacing: 16) {
        VStack(alignment: .leading, spacing: 6) {
          Text(
            store.home.proposals.isEmpty
              ? "Make Limmr yours" : "Your AI suggested \(store.home.proposals.count) circles"
          ).font(.headline)
          Text(detail).font(.callout).foregroundStyle(AccessibleSecondary()).fixedSize(
            horizontal: false, vertical: true)
        }
        Spacer(minLength: 0)
        VStack(alignment: .trailing, spacing: 8) {
          Button(store.home.proposals.isEmpty ? "Continue setup" : "Review") {
            if store.home.proposals.isEmpty {
              store.setupStep = SetupFlow.Step.who.rawValue
              store.showSetup = true
            } else {
              store.showProposalReview = true
            }
          }.buttonStyle(.borderedProminent)
          Button("Not now") { dismissedReason = reason }
            .limmrButton(id: "not-now").font(.caption).foregroundStyle(AccessibleSecondary())
        }
      }.padding(16).background(.primary.opacity(0.025), in: RoundedRectangle(cornerRadius: 12))
    }
  }
  private var detail: String {
    if !store.home.proposals.isEmpty {
      return "Keep the circles that fit. Your AI can fill in their briefs next."
    }
    if reason == "about-you-empty" {
      return "Add About you in Settings, or let your AI help with three short questions."
    }
    return "Connect your AI and ask it to set up your circles and people."
  }
}
struct ProposalReview: View {
  @Bindable var store: AppStore
  @Environment(\.dismiss) private var dismiss
  private var batches: [String] {
    Array(Set(store.home.proposals.map { $0.batchId ?? $0.createdBy ?? $0.source })).sorted()
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 18) {
      HStack {
        Text("Suggested circles").font(.title.bold())
        Spacer()
        Button("Done") { dismiss() }.buttonStyle(.bordered)
      }
      Text("A few circles about life now. Keep what fits, edit a name, or dismiss a suggestion.")
        .font(.callout).foregroundStyle(AccessibleSecondary())
      ScrollView {
        LazyVStack(alignment: .leading, spacing: 22) {
          if store.home.proposals.isEmpty {
            Text("All reviewed. Your AI can write the first briefs now.").foregroundStyle(
              .secondary)
          }
          ForEach(batches, id: \.self) { batch in
            let proposals = store.home.proposals.filter {
              ($0.batchId ?? $0.createdBy ?? $0.source) == batch
            }
            VStack(alignment: .leading, spacing: 16) {
              HStack {
                Text(proposals.first?.createdBy.map { store.agentName($0) } ?? "Suggested circles")
                  .font(.callout).foregroundStyle(AccessibleSecondary())
                Spacer()
                Button("Keep all") {
                  Task { await store.reviewProposals(proposals.map(\.id), keep: true) }
                }.buttonStyle(.borderedProminent)
                Button("Dismiss all") {
                  Task { await store.reviewProposals(proposals.map(\.id), keep: false) }
                }.buttonStyle(.bordered)
              }.disabled(store.busy)
              ForEach(proposals) { proposal in
                ReviewProposalRow(store: store, proposal: proposal)
                if proposal.id != proposals.last?.id { Divider() }
              }
            }
          }
        }
      }
      if let count = store.setupStatus?.unsortedActiveChats, count > 0 {
        Text("\(count) active chats are outside circles. They can stay unsorted.")
          .font(.caption).foregroundStyle(AccessibleSecondary())
      }
      ActionFeedback(store: store, action: "proposal-review")
    }.padding(24).frame(width: 620).frame(minHeight: 320, idealHeight: 530, maxHeight: 530)
      .background(Color(nsColor: .windowBackgroundColor))
  }
}
private struct ReviewProposalRow: View {
  @Bindable var store: AppStore
  let proposal: CircleProposal
  @State private var editing = false
  @State private var name = ""
  var body: some View {
    VStack(alignment: .leading, spacing: 10) {
      Text(proposal.circle.name).font(.headline)
      Text(proposal.why).font(.callout).foregroundStyle(AccessibleSecondary())
      DisclosureGroup {
        CurrentChatSections(
          store: store,
          chats: store.chats.filter { chat in
            proposal.circle.chats.contains { $0.chatJid == chat.jid }
          }
        ) { chat in
          HStack {
            Text(chat.name)
            Spacer()
            if let at = chat.lastMessageTime {
              Text(store.relativeTime(at)).foregroundStyle(AccessibleSecondary())
            }
          }.font(.callout).padding(.vertical, 4)
        }
      } label: {
        Text("\(proposal.circle.chats.count) chats · \(store.proposalActivity(proposal))").font(
          .caption
        ).foregroundStyle(AccessibleSecondary())
      }

      if editing { TextField("Circle name", text: $name).textFieldStyle(.roundedBorder) }
      HStack(spacing: 16) {
        Button("Keep") {
          Task {
            if editing {
              await store.keepProposal(proposal.id, name: name)
            } else {
              await store.reviewProposals([proposal.id], keep: true)
            }
          }
        }.buttonStyle(.bordered).disabled(
          editing && name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
        Button(editing ? "Cancel edit" : "Edit") {
          name = proposal.circle.name
          editing.toggle()
        }.limmrButton()
        Button("Dismiss") { Task { await store.reviewProposals([proposal.id], keep: false) } }
          .limmrButton()
      }.disabled(store.busy)
      ActionFeedback(store: store, action: "circle-\(proposal.id)")
    }
  }
}
