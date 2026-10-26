import Foundation
import LimmrKit

/// Capacity-aware regions seed the anchors; all dots still come from one even field.
/// Matching the whole field at once avoids the long tails produced by round-robin claims.
enum DriftRegions {
  static func seeds(ids: [String], counts: [String: Int], field: CGRect) -> [String: CGPoint] {
    var result: [String: CGPoint] = [:]
    // Quiet circles still need space for a measured name and its button.
    let weights = Dictionary(uniqueKeysWithValues: ids.map { ($0, Double(max(6, counts[$0] ?? 0))) })
    func partition(_ ids: ArraySlice<String>, _ box: CGRect) {
      guard let first = ids.first else { return }
      if ids.count == 1 {
        let hash = MapSceneMotion.stableHash(first)
        let dx = (Double(hash % 1009) / 1008 - 0.5) * 0.32
        let dy = (Double((hash / 1009) % 1013) / 1012 - 0.5) * 0.38
        result[first] = CGPoint(x: box.midX + box.width * dx, y: box.midY + box.height * dy)
        return
      }
      let total = ids.reduce(0) { $0 + weights[$1]! }
      var sum = 0.0, split = 1, difference = Double.infinity
      for index in 1..<ids.count {
        sum += weights[ids[ids.startIndex + index - 1]]!
        if abs(total / 2 - sum) < difference { difference = abs(total / 2 - sum); split = index }
      }
      let left = ids.prefix(split), right = ids.dropFirst(split)
      let share = left.reduce(0) { $0 + weights[$1]! } / total
      if box.width > box.height {
        let width = box.width * share
        partition(left, CGRect(x: box.minX, y: box.minY, width: width, height: box.height))
        partition(right, CGRect(x: box.minX + width, y: box.minY, width: box.width - width, height: box.height))
      } else {
        let height = box.height * share
        partition(left, CGRect(x: box.minX, y: box.minY, width: box.width, height: height))
        partition(right, CGRect(x: box.minX, y: box.minY + height, width: box.width, height: box.height - height))
      }
    }
    partition(ids[...], field)
    return result
  }

  /// A soft region's radius depends on capacity and field area, never on its longest line.
  static func radius(count: Int, total: Int, area: Double) -> Double {
    32 + 1.6 * sqrt(area * Double(max(6, count)) / Double(max(1, total)) / .pi)
  }

  /// Rectangular Hungarian assignment. Repeated anchors are capacity slots, not new dots.
  /// Distance is normalised by region radius so quiet circles keep their nearest points
  /// while populous circles occupy a wider area. Stable inputs also resolve equal-cost ties.
  static func assign(anchors: [CGPoint], radii: [Double], samples: [CGPoint]) -> [Int] {
    let count = anchors.count, columns = samples.count
    guard count > 0, columns >= count else { return [] }
    let costs = anchors.enumerated().map { index, anchor in
      samples.map { point in
        let distance = hypot(point.x - anchor.x, point.y - anchor.y) / radii[index]
        return distance * distance + max(0, distance - 1) * 100
      }
    }
    var u = Array(repeating: 0.0, count: count + 1), v = Array(repeating: 0.0, count: columns + 1)
    var matched = Array(repeating: 0, count: columns + 1), way = matched
    for row in 1...count {
      matched[0] = row
      var column = 0
      var minimum = Array(repeating: Double.infinity, count: columns + 1)
      var used = Array(repeating: false, count: columns + 1)
      repeat {
        used[column] = true
        let current = matched[column]
        var delta = Double.infinity, next = 0
        for j in 1...columns where !used[j] {
          let cost = costs[current - 1][j - 1] - u[current] - v[j]
          if cost < minimum[j] { minimum[j] = cost; way[j] = column }
          if minimum[j] < delta { delta = minimum[j]; next = j }
        }
        for j in 0...columns {
          if used[j] { u[matched[j]] += delta; v[j] -= delta }
          else { minimum[j] -= delta }
        }
        column = next
      } while matched[column] != 0
      repeat {
        let previous = way[column]
        matched[column] = matched[previous]
        column = previous
      } while column != 0
    }
    var result = Array(repeating: 0, count: count)
    for column in 1...columns where matched[column] > 0 { result[matched[column] - 1] = column - 1 }
    return result
  }
}
