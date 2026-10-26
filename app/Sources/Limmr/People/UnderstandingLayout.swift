import CoreText
import SwiftUI
import LimmrKit

/// Deterministic Drift field and full circle focus. Geometry and measured labels are shared by drawing and hit testing.
struct UnderstandingLayout: Sendable {
  private(set) var positions: [String: CGPoint] = [:]
  private(set) var labels: [String: CGRect] = [:]
  private(set) var labelText: [String: String] = [:]
  private(set) var size = CGSize.zero
  private(set) var clusters: [String: CGRect] = [:]
  private(set) var overviewContentHeight = 280.0
  private(set) var unaffiliatedBounds = CGRect.zero
  private(set) var recency: [String: Double] = [:]
  private(set) var regionRadii: [String: Double] = [:]
  static func footprint(_ node: CircleMapNode, at point: CGPoint) -> CGRect {
    let r = node.kind == "circle" ? 14.0 : max(6, radius(node) + 3)
    return CGRect(x: point.x-r, y: point.y-r, width: r*2, height: r*2)
  }
  static func memberships(_ node: CircleMapNode, map: CircleMap) -> [String] {
    let circles = Set(map.nodes.filter { $0.kind == "circle" }.map(\.id))
    let links = map.edges.filter { $0.target == node.id && $0.state != "reverted" }.map(\.source)
    return Set((node.circles ?? []) + [node.primaryCircleId, node.circleId].compactMap { $0 } + links)
      .filter { circles.contains($0) }.sorted()
  }
  static func primary(_ node: CircleMapNode, map: CircleMap) -> String? {
    let ids = memberships(node, map: map)
    if let id = node.primaryCircleId, ids.contains(id) { return id }
    if let id = node.circleId, ids.contains(id) { return id }
    return ids.first
  }
  static func recent(_ a: CircleMapNode, _ b: CircleMapNode) -> Bool {
    let x = a.lastActiveAt.flatMap(Timestamp.date) ?? .distantPast
    let y = b.lastActiveAt.flatMap(Timestamp.date) ?? .distantPast
    if x != y { return x > y }
    if a.weight != b.weight { return (a.weight ?? 0) > (b.weight ?? 0) }
    return a.id < b.id
  }
  static func radius(_ node: CircleMapNode) -> Double {
    node.kind == "circle" ? 12 : node.kind == "chat" ? 3 : (node.weight ?? 0) > 0.66 ? 5 : (node.weight ?? 0) > 0.2 ? 3.8 : 2.7
  }
  mutating func update(map: CircleMap, size: CGSize, focused: Bool = false) {
    guard size.width > 0, size.height > 0 else { return }
    if !focused { updateOverview(map: map, size: size); return }
    let previous = self.size == size ? positions : [:]
    self.size = size
    positions = [:]; labels = [:]; labelText = [:]; clusters = [:]; regionRadii = [:]
    let circles = map.nodes.filter { $0.kind == "circle" }.sorted { $0.id < $1.id }
    let dates = Dictionary(uniqueKeysWithValues: map.nodes.map { ($0.id, $0.lastActiveAt.flatMap(Timestamp.date) ?? .distantPast) })
    let memberships = Dictionary(uniqueKeysWithValues: map.nodes.map { ($0.id, Self.memberships($0,map:map)) })
    let primary = Dictionary(uniqueKeysWithValues: map.nodes.map { ($0.id, Self.primary($0,map:map) ?? "") })
    let ordered = map.nodes.sorted { a,b in
      if dates[a.id] != dates[b.id] { return dates[a.id]! > dates[b.id]! }
      return a.weight == b.weight ? a.id < b.id : (a.weight ?? 0) > (b.weight ?? 0)
    }
    // Reserve the edge lane even when empty, so a new unaffiliated person never moves the hubs.
    let inset = 32.0
    let usable = CGRect(x: inset, y: inset, width: max(1, size.width - inset*2 - 60), height: max(1, size.height - inset*2))
    // Sunflower seeds, followed by repulsion and a weak centre force. No rows or columns.
    var hubs = circles.enumerated().map { i, _ in
      let angle = Double(i) * 2.399963229728653
      let r = sqrt((Double(i)+0.5) / Double(max(1, circles.count)))
      return CGPoint(x: usable.midX + cos(angle)*r*usable.width*0.44, y: usable.midY + sin(angle)*r*usable.height*0.44)
    }
    if circles.count == 1 { hubs[0] = CGPoint(x: usable.midX, y: usable.midY) }
    let spacing = sqrt(usable.width * usable.height / Double(max(1, circles.count))) * 0.94
    for _ in 0..<180 {
      var next = hubs
      for i in hubs.indices {
        var dx = (usable.midX-hubs[i].x)*0.003, dy = (usable.midY-hubs[i].y)*0.003
        for j in hubs.indices where i != j {
          let x = hubs[i].x-hubs[j].x, y = hubs[i].y-hubs[j].y
          let d = max(0.1, hypot(x,y)), push = max(0, spacing-d)*0.14
          dx += x/d*push; dy += y/d*push
        }
        next[i] = CGPoint(x: min(usable.maxX, max(usable.minX, hubs[i].x+dx)), y: min(usable.maxY, max(usable.minY, hubs[i].y+dy)))
      }
      hubs = next
    }
    for (i, node) in circles.enumerated() { positions[node.id] = hubs[i] }
    unaffiliatedBounds = CGRect(x: size.width-55, y: 8, width: 47, height: size.height-16)
    var occupied = circles.compactMap { n in positions[n.id].map { Self.footprint(n, at: $0) } }
    for circle in circles {
      let anchor = positions[circle.id]!
      let members = ordered.filter { $0.kind != "circle" && primary[$0.id] == circle.id }
      let nearest = hubs.filter { $0 != anchor }.map { hypot($0.x-anchor.x, $0.y-anchor.y) }.min() ?? min(size.width, size.height)
      let available = min(nearest*0.47, anchor.x-8, size.width-anchor.x-8, anchor.y-8, size.height-anchor.y-8)
      var previousRadius = 22.0
      for (rank, node) in members.enumerated() {
        let ids = memberships[node.id] ?? []
        let second = ids.first { $0 != circle.id }.flatMap { positions[$0] }
        let baseAngle = second.map { atan2($0.y-anchor.y, $0.x-anchor.x) } ?? Double(MapSceneMotion.stableHash(node.id)%10000)/10000 * .pi*2
        let desired = 23 + Double(rank)/Double(max(1,members.count-1))*max(12, available-24)
        var point: CGPoint?
        // Monotonic radius encodes recency, including when collision repair is needed.
        for step in 0..<180 {
          let r = max(previousRadius+0.15, desired) + Double(step/36)*4
          let angle = baseAngle + (second == nil ? Double(step%36)*2.399963 : (Double(step%36)-18)*0.025)
          let candidate = CGPoint(x: anchor.x+cos(angle)*r, y: anchor.y+sin(angle)*r)
          let box = Self.footprint(node, at: candidate)
          if CGRect(origin: .zero, size: size).insetBy(dx: 2, dy: 2).contains(box), !occupied.contains(where: { $0.intersects(box) }) {
            point = candidate; previousRadius = r; break
          }
        }
        if point == nil {
          // Crowded edge clusters get another full angular search, never a missing dot.
          for step in 0..<2000 {
            let r = max(previousRadius+0.2, desired) + Double(step/180)*3
            let a = baseAngle+Double(step%180)*2.399963
            let candidate = CGPoint(x: anchor.x+cos(a)*r, y: anchor.y+sin(a)*r)
            let box = Self.footprint(node, at: candidate)
            if CGRect(origin: .zero, size: size).contains(box), !occupied.contains(where: { $0.intersects(box) }) { point = candidate; previousRadius = r; break }
          }
        }
        if let point { positions[node.id] = point; occupied.append(Self.footprint(node, at: point)) }
      }
      let points = members.compactMap { positions[$0.id] } + [anchor]
      clusters[circle.id] = points.reduce(CGRect(x: anchor.x, y: anchor.y, width: 1, height: 1)) { $0.union(CGRect(x: $1.x, y: $1.y, width: 1, height: 1)) }.insetBy(dx: -7, dy: -7)
    }
    let unaffiliated = ordered.filter { $0.kind != "circle" && primary[$0.id] == "" }
    for node in unaffiliated {
      if let old = previous[node.id], unaffiliatedBounds.contains(old) { positions[node.id] = old }
    }
    for node in unaffiliated where positions[node.id] == nil {
      for i in 0..<max(3,unaffiliated.count*3) {
        let candidate = CGPoint(x: unaffiliatedBounds.midX + Double(i%3-1)*13, y: 16+Double(i/3)*14)
        if !unaffiliated.contains(where: { other in positions[other.id].map { hypot($0.x-candidate.x,$0.y-candidate.y) < 12 } == true }) {
          positions[node.id] = candidate; break
        }
      }
    }
    placeLabels(map: map, focused: focused)
  }
  static func badge(at point: CGPoint) -> CGRect {
    CGRect(x: point.x+12, y: point.y-28, width: 42, height: 24)
  }
  mutating func placeLabels(map: CircleMap, focused: Bool, extra: String? = nil) {
    if !focused { placeOverviewLabels(map: map, extra: extra); return }
    labels = [:]; labelText = [:]
    let circles = map.nodes.filter { $0.kind == "circle" }.sorted { $0.id < $1.id }
    let badges = focused ? [] : circles.compactMap { positions[$0.id].map(Self.badge) }
    let dates = Dictionary(uniqueKeysWithValues: map.nodes.map { ($0.id, $0.lastActiveAt.flatMap(Timestamp.date) ?? .distantPast) })
    let recent = map.nodes.sorted { a,b in dates[a.id] == dates[b.id] ? a.id < b.id : dates[a.id]! > dates[b.id]! }
    var priority = circles
    for circle in circles {
      priority += recent.filter { $0.kind == "person" && Self.memberships($0, map: map).contains(circle.id) }.prefix(focused ? .max : 2)
    }
    if let extra, let node = map.nodes.first(where: { $0.id == extra }), !priority.contains(where: { $0.id == extra }) { priority.append(node) }
    for node in priority where labels[node.id] == nil {
      guard let point = positions[node.id] else { continue }
      let font = Self.font(size: node.kind == "circle" ? 11 : 10)
      let text = node.kind == "circle" ? Self.wrap(node.title, font: font, width: 135) : node.title
      let lines = text.components(separatedBy: "\n")
      let width = ceil(lines.map { Self.measure($0, font: font) }.max() ?? 0)+6
      let height = Double(lines.count)*14+4
      func free(_ rect: CGRect) -> Bool {
        CGRect(origin: .zero, size: size).insetBy(dx: 3, dy: 3).contains(rect) && !labels.values.contains { $0.insetBy(dx: -2, dy: -2).intersects(rect) } && !badges.contains { $0.intersects(rect) }
      }
      var chosen: CGRect?
      for step in 0..<720 {
        let r = 20 + Double(step/24)*6
        let angle = .pi/2 + Double(step%24)*(.pi/12)
        let rect = CGRect(x: point.x+cos(angle)*r-width/2, y: point.y+sin(angle)*r-height/2, width: width, height: height)
        if free(rect) { chosen = rect; break }
      }
      if let chosen { labels[node.id] = chosen; labelText[node.id] = text }
    }
  }
  static func font(size: Double) -> CTFont {
    let base = CTFontCreateUIFontForLanguage(.system, size, nil)!
    let descriptor = CTFontDescriptorCreateCopyWithAttributes(CTFontCopyFontDescriptor(base),
      [kCTFontTraitsAttribute: [kCTFontWeightTrait: 0.23]] as CFDictionary)
    return CTFontCreateWithFontDescriptor(descriptor, size, nil)
  }
  static func measure(_ text: String, font: CTFont) -> Double {
    let value = NSAttributedString(string: text, attributes: [NSAttributedString.Key(kCTFontAttributeName as String): font])
    return CTLineGetTypographicBounds(CTLineCreateWithAttributedString(value), nil, nil, nil)
  }
  static func wrap(_ text: String, font: CTFont, width: Double) -> String {
    let words = text.split(separator: " ")
    guard Self.measure(text, font: font) > width, words.count > 1 else { return text }
    let candidates = (1..<words.count).map { i in [words[..<i].joined(separator:" "), words[i...].joined(separator:" ")] }
    let best = candidates.min { a,b in a.map(\.count).max()! < b.map(\.count).max()! }!
    return best.joined(separator: "\n")
  }
  func displayed(_ id: String, time: Double) -> CGPoint? { positions[id] }
  func hit(_ point: CGPoint, map: CircleMap) -> MapHit? {
    guard CGRect(origin: .zero, size: size).insetBy(dx: 8, dy: 8).contains(point) else { return nil }
    // Labels win even if the soft region overlaps another circle's dots.
    for node in map.nodes where labels[node.id]?.contains(point) == true { return .node(node.id) }
    if let nearest = map.nodes.filter({ $0.kind == "person" && positions[$0.id] != nil }).min(by: { a,b in
      let x=positions[a.id]!, y=positions[b.id]!
      return hypot(x.x-point.x,x.y-point.y) < hypot(y.x-point.x,y.y-point.y)
    }), let p = positions[nearest.id], hypot(p.x-point.x,p.y-point.y) < max(8,Self.radius(nearest)) { return .node(nearest.id) }
    // Soft regions can overlap: choose the nearest normalised anchor deterministically.
    let circles = map.nodes.filter { $0.kind == "circle" && positions[$0.id] != nil }
    let nearest = circles.sorted { a, b in
      let da = regionDistance(point, id: a.id), db = regionDistance(point, id: b.id)
      return da == db ? a.id < b.id : da < db
    }.first
    if let nearest, regionDistance(point, id: nearest.id) <= 1 { return .node(nearest.id) }
    return nil
  }
  private func regionDistance(_ point: CGPoint, id: String) -> Double {
    guard let anchor = positions[id] else { return .infinity }
    return hypot(anchor.x-point.x, anchor.y-point.y) / max(32, regionRadii[id] ?? 80)
  }



}
// Drift reserves measured labels first, then samples one blue-noise field around them.
// Membership assigns existing field points; it never compresses them into clusters.
extension UnderstandingLayout {
  static let minimumDotSpacing = 18.0

  static func overviewHeight(map: CircleMap, width: Double) -> Double {
    var layout = Self()
    layout.updateOverview(map: map, size: CGSize(width: max(1, width), height: 280))
    return layout.overviewContentHeight
  }

  func overviewBadge(_ id: String) -> CGRect? {
    guard let label = labels[id] else { return nil }
    return CGRect(x: label.maxX + 3, y: label.midY - 10, width: 38, height: 20)
  }

  private mutating func updateOverview(map: CircleMap, size requested: CGSize) {
    positions = [:]; labels = [:]; labelText = [:]; clusters = [:]; regionRadii = [:]
    let circles = map.nodes.filter { $0.kind == "circle" }.sorted { $0.id < $1.id }
    let people = map.nodes.filter { $0.kind != "circle" }.sorted { $0.id < $1.id }
    recency = Dictionary(uniqueKeysWithValues: people.sorted(by: Self.recent).enumerated().map {
      ($0.element.id, 1 - Double($0.offset) / Double(max(1, people.count - 1)))
    })
    // Extra rows are only needed for exceptionally many circles / narrow standalone canvases.
    let columns = max(1, Int(max(1, requested.width - 32) / 175))
    let rows = max(1, (circles.count + columns - 1) / columns)
    overviewContentHeight = max(280, Double(rows) * 78 + 32)
    size = CGSize(width: requested.width, height: overviewContentHeight)
    let field = CGRect(origin: .zero, size: size).insetBy(dx: 18, dy: 28)
    let primary = Dictionary(uniqueKeysWithValues: people.map { ($0.id, Self.primary($0, map: map) ?? "") })
    let counts = Dictionary(grouping: people, by: { primary[$0.id]! }).mapValues(\.count)
    let seeds = DriftRegions.seeds(ids: circles.map(\.id), counts: counts, field: field)
    let capacity = circles.reduce(0) { $0 + max(6, counts[$1.id] ?? 0) } + (counts[""] ?? 0)
    for circle in circles {
      let anchor = seeds[circle.id]!
      regionRadii[circle.id] = DriftRegions.radius(count: counts[circle.id] ?? 0, total: capacity, area: field.width * field.height)
      let text = Self.wrap(circle.title, font: Self.font(size: 11), width: min(128, max(40, field.width - 48)))
      let lines = text.components(separatedBy: "\n")
      let width = min(max(1, size.width - 52), ceil(lines.map { Self.measure($0, font: Self.font(size: 11)) }.max() ?? 0) + 10)
      let height = Double(lines.count) * 14 + 6
      // The badge is included in the search envelope. Labels choose the nearest quiet slot.
      for step in 0..<2000 {
        let radius = Double(step / 24) * 4
        let angle = Double(step % 24) * .pi / 12
        let box = CGRect(x: anchor.x + cos(angle)*radius - (width+41)/2,
          y: anchor.y + sin(angle)*radius - height/2, width: width, height: height)
        let envelope = box.union(CGRect(x: box.maxX+3, y: box.midY-10, width: 38, height: 20))
        if CGRect(origin: .zero, size: size).insetBy(dx: 8, dy: 12).contains(envelope),
          !labels.keys.contains(where: { id in
            labels[id]!.union(overviewBadge(id)!).insetBy(dx: -8, dy: -8).intersects(envelope)
          }) {
          labels[circle.id] = box; labelText[circle.id] = text
          positions[circle.id] = CGPoint(x: box.midX, y: box.midY)
          break
        }
      }
    }
    let obstacles = labels.keys.sorted().compactMap { id in
      labels[id].map { $0.union(overviewBadge(id)!).insetBy(dx: -9, dy: -9) }
    }
    // Fixed integer PRNG and stable iteration: refresh and input ordering cannot jitter the field.
    var seed: UInt64 = 0x6472696674
    func unit() -> Double {
      seed = seed &* 6364136223846793005 &+ 1442695040888963407
      return Double(seed >> 11) / 9007199254740992
    }
    var samples: [CGPoint] = []
    for _ in people {
      var best: CGPoint?, score = -Double.infinity
      for _ in 0..<240 {
        let p = CGPoint(x: field.minX + unit()*field.width, y: field.minY + unit()*field.height)
        guard !obstacles.contains(where: { $0.contains(p) }) else { continue }
        let distance = samples.reduce(Double.infinity) {
          min($0, ($1.x-p.x)*($1.x-p.x) + ($1.y-p.y)*($1.y-p.y))
        }
        if distance >= Self.minimumDotSpacing * Self.minimumDotSpacing && distance > score { best = p; score = distance }
      }
      if let best { samples.append(best) }
    }
    // Solve all capacity claims together. A busy circle gets a broad soft region,
    // rather than consuming every other circle's nearby dots one rank at a time.
    let grouped = circles.map { circle in people.filter { primary[$0.id] == circle.id }.sorted(by: Self.recent) }
    let slots = grouped.enumerated().flatMap { index, members in Array(repeating: index, count: members.count) }
    let unplaced = people.filter { primary[$0.id] == "" }
    let assignment = DriftRegions.assign(
      anchors: slots.map { positions[circles[$0].id]! },
      radii: slots.map { regionRadii[circles[$0].id]! }, samples: samples)
    var claimed = Set<Int>()
    for (index, members) in grouped.enumerated() {
      let anchor = positions[circles[index].id]!
      let points = slots.indices.filter { slots[$0] == index }.compactMap { slot -> CGPoint? in
        guard slot < assignment.count else { return nil }
        claimed.insert(assignment[slot])
        return samples[assignment[slot]]
      }.sorted {
        let a = hypot($0.x-anchor.x, $0.y-anchor.y), b = hypot($1.x-anchor.x, $1.y-anchor.y)
        return a == b ? ($0.x == $1.x ? $0.y < $1.y : $0.x < $1.x) : a < b
      }
      for (node, point) in zip(members, points) { positions[node.id] = point }
    }
    for (node, index) in zip(unplaced, samples.indices.filter { !claimed.contains($0) }) {
      positions[node.id] = samples[index]
    }
    unaffiliatedBounds = CGRect(origin: .zero, size: size)
  }

  private mutating func placeOverviewLabels(map: CircleMap, extra: String? = nil) {
    // Labels and reserved dots were prepared together. Hover never changes the picture.
  }

  func driftEmphasis(_ node: CircleMapNode, map: CircleMap) -> Double {
    guard let primary = Self.primary(node, map: map), let a = positions[primary], let p = positions[node.id] else { return 0.12 }
    return max(0.18, 1 - hypot(a.x-p.x, a.y-p.y) / max(90, size.width * 0.28))
  }
}

struct MapArrival { var event: MapEvent; var at: Date }
enum MapHit: Hashable { case node(String) }
struct MapSelection: Equatable {
  var person: String?
  var circle: String?
  var tab: String? { person != nil ? "People" : circle != nil ? "Circles" : nil }
  mutating func click(_ node: CircleMapNode?) -> String? {
    guard let node else { self = MapSelection(); return nil }
    if node.kind == "person" { let open = person == node.id; person = node.id; return open ? node.id : nil }
    if node.kind == "circle" { person = nil; circle = node.id }
    return nil
  }
}
enum UnderstandingPresentation {
  static func calmMap(_ map: CircleMap, selected: String? = nil, expanded: Bool = false, focus: String? = nil) -> CircleMap {
    var result = map
    let reference = map.nodes.compactMap { $0.lastActiveAt.flatMap(Timestamp.date) }.max() ?? .distantPast
    let scores = Dictionary(uniqueKeysWithValues: map.nodes.map { n in
      let age = n.lastActiveAt.flatMap(Timestamp.date).map { max(0,reference.timeIntervalSince($0)/86400) } ?? 365
      return (n.id, (1 + (n.weight ?? 0)*4)/(1+age/14))
    })
    let people = map.nodes.filter { $0.kind == "person" }.sorted {
      scores[$0.id] == scores[$1.id] ? $0.id < $1.id : scores[$0.id]! > scores[$1.id]!
    }
    if let focus {
      result.nodes = map.nodes.filter { $0.id == focus || ($0.kind != "circle" && UnderstandingLayout.memberships($0,map:map).contains(focus)) }
      for i in result.nodes.indices where result.nodes[i].kind != "circle" { result.nodes[i].primaryCircleId = focus }
    } else {
      var chosen = Set([selected].compactMap { $0 })
      for circle in map.nodes.filter({ $0.kind == "circle" }).sorted(by: { $0.id < $1.id }) {
        for p in people.filter({ UnderstandingLayout.memberships($0,map:map).contains(circle.id) }).prefix(3) where chosen.count < 120 { chosen.insert(p.id) }
      }
      for p in people where chosen.count < 120 { chosen.insert(p.id) }
      result.nodes = map.nodes.filter { $0.kind == "circle" || $0.kind == "chat" || (chosen.contains($0.id) && (expanded || $0.id == selected || UnderstandingLayout.primary($0,map:map) != nil)) }
    }
    let shown = Set(result.nodes.map(\.id))
    result.edges = map.edges.filter { shown.contains($0.source) && shown.contains($0.target) }
    return result
  }
  static func fittedLatest(_ text: String, width: Double) -> String {
    let font = UnderstandingLayout.font(size: 12)
    let full = "Latest: " + text
    if UnderstandingLayout.measure(full, font: font) <= width { return full }
    var result = "Latest:"
    for word in text.split(whereSeparator: \.isWhitespace) {
      let next = result + " " + word
      if UnderstandingLayout.measure(next + "…", font: font) > width { break }
      result = next
    }
    return result + "…"
  }
  static func wordEllipsis(_ text: String, limit: Int) -> String {
    guard text.count > limit else { return text }
    let words = text.split(whereSeparator: \.isWhitespace)
    var result = ""
    for word in words {
      let next = result.isEmpty ? String(word) : result+" "+word
      if next.count+1 > limit { break }; result = next
    }
    return result + "…"
  }
  static func nodeText(_ node: CircleMapNode, circles: [LimmrKit.Circle], now: Date) -> String {
    let roles = circles.compactMap { c in c.people.first { $0.personId == node.id }.map { "\($0.role) · \(c.name)" } }
    let reasons = (node.circleAffinity ?? []).map { a in (circles.first { $0.id == a.circleId }?.name ?? "Circle")+": "+a.reasons.joined(separator:"; ") }
    let groups = node.groupsInCommon ?? []
    let common = groups.isEmpty && (node.groupsInCommonCount ?? 0) == 0 ? [] : ["Groups in common (\(node.groupsInCommonCount ?? groups.count))"] + groups.map { g in
      let tags = circles.filter { g.circleIds.contains($0.id) }.map(\.name)
      return g.name + (tags.isEmpty ? "" : " · "+tags.joined(separator:", "))
    } + ((node.groupsInCommonCount ?? groups.count) > groups.count ? ["Open profile to see all groups"] : [])
    let active = node.lastActiveAt.flatMap(Timestamp.date).map { "last active · last contact \(max(0,Int(now.timeIntervalSince($0)/86400))) d ago" } ?? "Last contact unknown"
    return ([node.title]+[node.howKnown].compactMap { $0 }.filter { !$0.isEmpty }+roles+reasons+common+[active]).joined(separator:"\n")
  }
}
