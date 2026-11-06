import LimmrKit

extension AppStore {
  func closePeopleInspector() { mapSelection.person = nil }
  /// Map labels open the same destination as the accessible circle list.
  /// Person selection stays on People and opens its inspector.
  func selectPeopleMapNode(_ node: CircleMapNode?) {
    if let node, node.kind == "circle" {
      openPeopleRoute(.circle(node.id))
      return
    }
    _ = mapSelection.click(node)
    if let node, node.kind == "person" { selectedPerson = node.id }
  }
}
