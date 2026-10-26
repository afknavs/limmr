// A deterministic suspended request. Test-owned tasks must be released and
// awaited before the test returns so failures cannot become orphan issues.
actor TestGate {
  private var entered = false
  private var released = false
  private var observers: [CheckedContinuation<Void, Never>] = []
  private var requests: [CheckedContinuation<Void, Never>] = []
  func enter() async {
    entered = true
    observers.forEach { $0.resume() }; observers.removeAll()
    if !released { await withCheckedContinuation { requests.append($0) } }
  }
  func waitUntilEntered() async {
    if !entered { await withCheckedContinuation { observers.append($0) } }
  }
  func release() {
    released = true
    requests.forEach { $0.resume() }; requests.removeAll()
  }
}
