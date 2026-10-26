import SwiftUI
import LimmrKit

/// Full circle membership supplies the count; bounded map and directory supply known names.
struct CircleHoverPreview {
  let title: String
  let names: [String]
  let total: Int
  var remaining: Int { max(0, total - names.count) }
  var spoken: String {
    let list = names.joined(separator: ", ")
    return "\(title), \(total) \(total == 1 ? "person" : "people")" + (list.isEmpty ? "" : ": \(list)")
      + (remaining > 0 ? ", and \(remaining) more" : "")
  }
  var cardHeight: Double { 50 + Double(names.count) * 18 + (remaining > 0 || total == 0 ? 22 : 0) }
  init(id: String, circles: [LimmrKit.Circle], people: [Person], map: CircleMap) {
    let circle = circles.first { $0.id == id }
    title = circle?.name ?? map.nodes.first { $0.id == id }?.title ?? "Circle"
    let linked = Set(map.edges.filter { $0.source == id && $0.state != "reverted" }.map(\.target))
    let mapped = map.nodes.filter {
      $0.kind == "person" && (linked.contains($0.id) || $0.circles?.contains(id) == true || $0.primaryCircleId == id || $0.circleId == id)
    }
    let ids = Set((circle?.people.map(\.personId) ?? []) + mapped.map(\.id))
    total = ids.count
    var known = Dictionary(uniqueKeysWithValues: mapped.map { ($0.id, $0.title) })
    for person in people where ids.contains(person.id) {
      known[person.id] = person.label?.isEmpty == false ? person.label! : person.name
    }
    names = known.sorted { a, b in
      a.value == b.value ? a.key < b.key : a.value.localizedStandardCompare(b.value) == .orderedAscending
    }.prefix(8).map { $0.value }
  }
  static func cardOrigin(label: CGRect, canvas: CGSize, height: Double) -> CGPoint {
    var x = min(max(8, label.minX), max(8, canvas.width - 216))
    var y = label.maxY + 8
    if y + height > canvas.height - 8 {
      y = label.minY - height - 8
      if y < 8 {
        // A tall preview sits beside a central label, keeping its box visible.
        if label.maxX + 216 <= canvas.width - 8 { x = label.maxX + 8 }
        else if label.minX - 216 >= 8 { x = label.minX - 216 }
        y = label.minY
      }
    }
    return CGPoint(x: x, y: min(max(8, y), max(8, canvas.height - height - 8)))
  }
}

struct CircleHoverCard: View {
  let preview: CircleHoverPreview
  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(preview.title).font(.caption.weight(.semibold)).lineLimit(2).padding(.bottom, 4)
      ForEach(Array(preview.names.enumerated()), id: \.offset) { _, name in
        Text(name).font(.caption).lineLimit(1).truncationMode(.tail)
      }
      if preview.remaining > 0 {
        Text("and \(preview.remaining) more").font(.caption).foregroundStyle(AccessibleSecondary()).padding(.top, 4)
      } else if preview.total == 0 {
        Text("No people yet").font(.caption).foregroundStyle(AccessibleSecondary())
      }
    }.padding(12).frame(maxWidth: .infinity, minHeight: preview.cardHeight, alignment: .topLeading)
      .background(Color(nsColor: .windowBackgroundColor), in: RoundedRectangle(cornerRadius: 9))
      .overlay(RoundedRectangle(cornerRadius: 9).stroke(.primary.opacity(0.25), lineWidth: 0.5))
      .shadow(color: .black.opacity(0.12), radius: 6, y: 2)
      .accessibilityElement(children: .ignore).accessibilityLabel(preview.spoken)
  }
}

/// The map footer uses one number-above-label hierarchy.
struct PeopleMapCountLabel: View {
  let value: Int
  let title: String
  var body: some View {
    VStack(alignment: .leading, spacing: 4) {
      Text(value.formatted()).font(.title3.weight(.semibold)).monospacedDigit().lineLimit(1)
      Text(title).font(.caption).foregroundStyle(AccessibleSecondary()).lineLimit(1)
    }.accessibilityElement(children: .ignore).accessibilityLabel(Spoken.count(value, title))
  }
}
