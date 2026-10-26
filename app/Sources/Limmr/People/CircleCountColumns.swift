import SwiftUI

/// One trailing grid for the header and every circle row, including an empty pin slot.
struct CircleCountColumns: View {
  static let membersWidth: CGFloat = 72
  static let newWidth: CGFloat = 56
  static let pinWidth: CGFloat = 20
  let pinned: Bool
  let members: Int
  let new: Int
  var body: some View {
    HStack(alignment: .firstTextBaseline, spacing: 16) {
      Image(systemName: "pin.fill").font(.caption).foregroundStyle(AccessibleSecondary())
        .opacity(pinned ? 1 : 0).frame(width: Self.pinWidth)
      Text(members.formatted()).frame(width: Self.membersWidth, alignment: .trailing)
      Text(new > 0 ? new.formatted() : "").foregroundStyle(new > 0 ? Color.accentColor : .secondary)
        .frame(width: Self.newWidth, alignment: .trailing)
    }.font(.callout).monospacedDigit().accessibilityHidden(true)
  }
  static var header: some View {
    HStack(spacing: 16) {
      Spacer(minLength: 0)
      Text("Members").frame(width: membersWidth, alignment: .trailing)
      Text("New").frame(width: newWidth, alignment: .trailing)
      Image(systemName: "chevron.right").font(.caption2).hidden()
    }.font(.caption).foregroundStyle(AccessibleSecondary()).padding(.vertical, 6).padding(.horizontal, 8)
  }
}
