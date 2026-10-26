import LimmrKit
import SwiftUI

struct AccessControls: View {
  @Binding var access: AgentAccess
  let circles: [LimmrKit.Circle]
  @State private var duration = "none"
  @State private var endDate = Date.now.addingTimeInterval(30 * 86400)
  var body: some View {
    VStack(alignment: .leading, spacing: 14) {
      Text("What it can see").font(.headline)
      Picker(
        "What it can see",
        selection: Binding(
          get: { access.circles == .all },
          set: { all in
            var next = access
            next.circles = all ? .all : .only([])
            next.unsorted = all
            access = next
          }
        )
      ) {
        Text("Everything").tag(true)
        Text("Only some circles").tag(false)
      }.pickerStyle(.segmented).labelsHidden()
      HStack {
        Text("For").font(.callout.weight(.semibold))
        Picker(
          "For",
          selection: Binding(
            get: { duration },
            set: { value in
              duration = value
              if value == "none" {
                access.until = nil
              } else if value == "until" {
                access.until = ISO8601DateFormatter().string(from: endDate)
              } else if let days = Double(value) {
                access.until = ISO8601DateFormatter().string(
                  from: .now.addingTimeInterval(days * 86400))
              }
            })
        ) {
          Text("No limit").tag("none")
          Text("7 days").tag("7")
          Text("30 days").tag("30")
          Text("90 days").tag("90")
          Text("Until…").tag("until")
        }.labelsHidden().fixedSize()
        Spacer()
      }
      if duration == "until" {
        DatePicker(
          "Access ends",
          selection: Binding(
            get: { endDate },
            set: { date in
              endDate = date
              access.until = ISO8601DateFormatter().string(from: date)
            }), displayedComponents: [.date, .hourAndMinute])
      }
      if case .only(let ids) = access.circles {
        LazyVGrid(
          columns: [GridItem(.adaptive(minimum: 180), alignment: .leading)], alignment: .leading,
          spacing: 10
        ) {
          ForEach(circles.filter(\.confirmed)) { circle in
            Toggle(
              circle.name,
              isOn: Binding(
                get: { ids.contains(circle.id) },
                set: { on in
                  var selected = ids.filter { $0 != circle.id }
                  if on { selected.append(circle.id) }
                  access.circles = .only(selected)
                }
              ))
          }
        }.toggleStyle(.checkbox)
        if circles.filter(\.confirmed).isEmpty {
          Text("Keep some circles first, or include chats in no circle.").font(.callout)
            .foregroundStyle(AccessibleSecondary())
        }
        Toggle("Chats in no circle", isOn: $access.unsorted).toggleStyle(.checkbox)
      }

    }
    .onAppear {
      if let date = access.until.flatMap(Timestamp.date) {
        endDate = date
        duration = "until"
      }
    }
    .onChange(of: access.until) { _, value in
      if let date = value.flatMap(Timestamp.date) {
        endDate = date
        duration = "until"
      } else {
        duration = "none"
      }
    }
  }
}
struct IntegrationAccess: View {
  @Bindable var store: AppStore
  let agent: Agent
  private var access: AgentAccess {
    store.permissionsAgentID == agent.id
      ? store.permissions.access ?? agent.access ?? AgentAccess()
      : agent.access ?? agent.permissions?.access ?? AgentAccess()
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack {
        Text("Access").font(.headline)
        Spacer()
        if access.hasEnded(at: store.demo ? Timestamp.date(DemoData.now)! : .now) {
          Text(store.accessSummary(access)).font(.callout).foregroundStyle(AccessibleSecondary())
        } else {
          Text("Sees " + store.accessSummary(access)).font(.callout).foregroundStyle(AccessibleSecondary())
        }
      }
      AccessControls(
        access: Binding(get: { access }, set: { store.updateIntegrationAccess($0, id: agent.id) }),
        circles: store.circles
      )
      .disabled(store.permissionsAgentID != agent.id || store.busy)
      HStack(spacing: 16) {
        Button("Renew") {
          var next = access
          next.until = ISO8601DateFormatter().string(from: .now.addingTimeInterval(30 * 86400))
          next.ended = false
          store.updateIntegrationAccess(next, id: agent.id)
        }
        Text("30 more days").font(.caption).foregroundStyle(AccessibleSecondary())
        Spacer()
        Button("End now") {
          var next = access
          next.until = ISO8601DateFormatter().string(
            from: store.demo ? Timestamp.date(DemoData.now)! : .now)
          next.ended = true
          store.updateIntegrationAccess(next, id: agent.id)
        }.disabled(access.hasEnded(at: store.demo ? Timestamp.date(DemoData.now)! : .now))
      }.buttonStyle(.bordered).disabled(store.permissionsAgentID != agent.id || store.busy)
      ActionFeedback(store: store, action: "access")
    }
  }
}
