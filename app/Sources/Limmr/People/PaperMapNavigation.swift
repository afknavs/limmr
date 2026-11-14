import Foundation

enum PaperMapNavigation {
  static func next(from current: String?, dx: Double, dy: Double, positions: [String: CGPoint], people: Set<String>) -> String? {
    guard let origin = current.flatMap({ positions[$0] }) else { return people.sorted().first { positions[$0] != nil } }
    return people.filter { $0 != current && positions[$0] != nil }.sorted().compactMap { id -> (String,Double)? in
      let point = positions[id]!, x = point.x-origin.x, y = point.y-origin.y, forward = x*dx+y*dy
      guard forward > 0.001 else { return nil }
      let length = hypot(x,y)
      return (id,length*length/forward)
    }.min { $0.1 < $1.1 }?.0 ?? current
  }
}
