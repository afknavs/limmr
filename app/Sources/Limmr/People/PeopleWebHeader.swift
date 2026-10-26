import SwiftUI
import LimmrKit

/// A map of what was learned, with the source of each connection kept visible.
struct PeopleWebHeader: View {
  @Bindable var store: AppStore
  @Environment(\.colorSchemeContrast) private var systemContrast
  @Environment(\.snapshotIncreasedContrast) private var snapshotContrast
  private var increasedContrast: Bool { systemContrast == .increased || snapshotContrast }
  @Environment(\.snapshotFocusedControl) private var snapshotFocus
  private var showsMapFocus: Bool { mapHasFocus || snapshotFocus == "map" }
  private var keyboardFocusNodeID: String? {
    keyboardNode.id ?? (snapshotFocus == "map" ? map.nodes.first { $0.kind == "person" }?.id : nil)
  }
  @State private var keyboardNode = KeyboardSelection()
  @State private var focusedMap: CircleMap?
  @State private var preparedMap = CircleMap.empty
  @State private var focusError: String?
  @State private var hoverDetail: PersonDetail?
  @FocusState private var labelFocus: String?
  @FocusState private var mapHasFocus: Bool
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  @Environment(\.snapshotRendering) private var snapshot
  @Environment(\.snapshotMapFrame) private var snapshotFrame
  private var focus: String? { store.mapSelection.circle }
  private var layout: UnderstandingLayout {
    if snapshot && ProcessInfo.processInfo.arguments.contains("--map-loading") { return UnderstandingLayout() }
    if snapshot {
      var value = UnderstandingLayout()
      value.update(map: map, size: size, focused: focus != nil)
      return value
    }
    return store.understandingLayout
  }
  @State private var size = CGSize.zero
  @State private var expanded = false
  @State private var hover: MapHit?
  @State private var pointer = CGPoint.zero
  @State private var replayStart: Date?
  @Environment(\.scenePhase) private var scenePhase
  @State private var frontmost = NSApp.isActive
  @State private var animating = false
  private var map: CircleMap { preparedMap }
  private var snapshotHoverID: String {
    map.nodes.first { $0.kind == "person" && $0.id.hasPrefix("p4-peer-") && UnderstandingLayout.memberships($0, map: map).count > 1 }?.id ?? "person-04"
  }
  private var events: [MapEvent] { map.events.sorted { $0.at < $1.at } }
  private var summaryMetrics: [(String, Int)] {
    let now = store.demo ? Timestamp.date(DemoData.now)! : Date()
    let edges = store.circleMap.edges.filter { $0.state != "reverted" }
    let aiWeek = edges.filter { e in e.origin?.byKind == "agent" && (e.origin?.at).flatMap(Timestamp.date).map { $0 <= now && $0 >= now.addingTimeInterval(-7*86400) } == true }.count
    return [
      ("People shown", map.nodes.filter { $0.kind == "person" }.count),
      ("Circles", map.nodes.filter { $0.kind == "circle" }.count),
      ("Links", store.circleMap.summary?.connections ?? edges.count),
      ("AI-found this week", store.circleMap.summary?.aiThisWeek ?? aiWeek),
    ]
  }
  private var hiddenCounts: [String: Int] {
    Dictionary(uniqueKeysWithValues: store.circles.map { circle in
      let visible = Set(map.nodes.filter { $0.kind == "person" && UnderstandingLayout.memberships($0, map: map).contains(circle.id) }.map(\.id))
      return (circle.id, max(0, Set(circle.people.map(\.personId)).subtracting(visible).count))
    })
  }

  private var unplaced: Int {
    store.circleMap.nodes.filter { node in
      node.kind == "person" && UnderstandingLayout.primary(node, map: store.circleMap) == nil
    }.count
  }
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      mapSurface
      if unplaced > 0 {
        Button { expanded.toggle() } label: {
          Label("Not in a circle yet · \(unplaced)", systemImage: expanded ? "chevron.down" : "chevron.right")
        }.font(.caption).limmrButton().foregroundStyle(AccessibleSecondary())
      }
      if focus != nil {
        Button("Back to the map", systemImage: "arrow.left") { store.mapSelection = MapSelection() }.font(.caption).limmrButton()
      }
      if let focusError { Text(focusError).font(.caption).foregroundStyle(AccessibleSecondary()) }

    }
    .onAppear {
      prepareMap()
      if !store.demo && !snapshot { PeopleMapStorage.removeRetiredSetting(in: RuntimePaths().home) }
    }
    .onChange(of: store.circleMap.nodes) { _, _ in prepareMap() }
    .onChange(of: store.circleMap.edges) { _, _ in prepareMap() }
    .onChange(of: store.circleMap.events) { _, _ in prepareMap() }
    .onChange(of: focusedMap?.nodes) { _, _ in prepareMap() }
    .onChange(of: focus) { _, _ in prepareMap() }
    .onChange(of: expanded) { _, _ in prepareMap() }
    .onChange(of: store.mapSelection.person) { _, _ in prepareMap() }
    .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in frontmost = true }
    .onReceive(NotificationCenter.default.publisher(for: NSApplication.didResignActiveNotification)) { _ in frontmost = false }
    .task(id: store.mapArrivals.count) {
      animating = !store.mapArrivals.isEmpty
      try? await Task.sleep(for: .seconds(4))
      animating = false
    }
  }
  private var mapDrawing: some View {
    let projection = layout
    return TimelineView(
        .animation(
          minimumInterval: 1 / 30, paused: snapshot || scenePhase != .active || !frontmost || (!animating && replayStart == nil) || reduceMotion)
      ) { timeline in
        let elapsed = replayStart.map { timeline.date.timeIntervalSince($0) }
        let progress = reduceMotion ? nil : snapshotFrame == "replay" ? 0.52 : elapsed.map { min(1, $0 / 8) }
        let time = reduceMotion || snapshot ? 0 : timeline.date.timeIntervalSinceReferenceDate
        let active = activeCircleID.map(MapHit.node) ?? (snapshotFrame == "hover" ? MapHit.node(snapshotHoverID) : (hover ?? (showsMapFocus ? keyboardFocusNodeID.map(MapHit.node) : nil) ?? store.mapSelection.person.map { MapHit.node($0) }))
        ZStack(alignment: .topLeading) {
          Canvas { context, canvasSize in
            // Neutral loading marks are not people and carry no interactive labels.
            for index in 0..<48 {
              let x = 24 + Double((index * 73 + 19) % 997) / 997 * max(0, canvasSize.width - 48)
              let y = 36 + Double((index * 131 + 41) % 991) / 991 * max(0, canvasSize.height - 72)
              context.fill(Path(ellipseIn: CGRect(x: x, y: y, width: 4, height: 4)), with: .color(.secondary.opacity(0.14)))
            }
          }.accessibilityHidden(true)
            .opacity(projection.positions.isEmpty ? 1 : 0)
            .animation(reduceMotion || snapshot ? nil : .easeOut(duration: 0.2), value: projection.positions.isEmpty)
          Canvas { context, canvasSize in
            var drawn = projection
            if snapshot && !ProcessInfo.processInfo.arguments.contains("--map-loading") && (drawn.size != canvasSize || drawn.positions.isEmpty) {
              drawn.update(map: map, size: canvasSize, focused: focus != nil)
            }
            if snapshot, case .node(let id) = active { drawn.placeLabels(map: map, focused: focus != nil, extra: id) }
            draw(context: &context, layout: drawn, time: time, now: timeline.date, progress: progress, active: active)
          } symbols: {
            ForEach(map.nodes.filter { $0.kind == "person" }) { node in
              PersonAvatar(store: store, id: node.id, name: node.title, size: 14).tag(node.id)
            }
          }
          .opacity(projection.positions.isEmpty ? 0 : 1)
          .animation(reduceMotion || snapshot ? nil : .easeIn(duration: 0.2), value: projection.positions.isEmpty)
          .accessibilityLabel(map.nodes.isEmpty ? "People map loading field" : "People map")
          .accessibilityChildren {
            ForEach(map.nodes.filter { $0.kind == "person" }) { node in
              Button(node.title) { select(node) }
                .accessibilityLabel(Spoken.node(node))
                .accessibilityValue(store.mapSelection.person == node.id || store.mapSelection.circle == node.id ? "Selected" : "Unselected")
                .accessibilityHint("Select this map node; activate a selected person again to open their profile")
            }
          }
          if map.nodes.isEmpty {
            VStack(alignment: .leading, spacing: 16) {
              Text(store.refreshing ? "Loading your circles…" : "Your map appears as circles sync.").font(.caption).foregroundStyle(AccessibleSecondary())
            }.padding(36).accessibilityLabel("Loading people map")
          } else if projection.positions.isEmpty {
            Text("Arranging your map…").font(.caption).foregroundStyle(AccessibleSecondary()).padding(20)
          }
          ForEach(map.nodes.filter { $0.kind == "circle" }) { circle in
            if let badge = projection.overviewBadge(circle.id), let count = hiddenCounts[circle.id], count > 0, focus == nil {
              Button { store.mapSelection.circle = circle.id; mapHasFocus = true } label: {
                Text(verbatim: "+\(count)").monospacedDigit().lineLimit(1).minimumScaleFactor(0.7)
              }
                .font(.system(size: 10, weight: .medium)).limmrButton()
                .padding(4).background(.regularMaterial, in: Capsule())
                .help("Show everyone in \(circle.title)")
                .accessibilityLabel("Show everyone in \(circle.title)")
                .accessibilityValue(Spoken.count(count, "additional people"))
                .accessibilityHint("Focus this circle on the map")
                .frame(width: badge.width, height: badge.height)
                .offset(x: badge.minX, y: badge.minY)
            }
          }
          ForEach(map.nodes.filter { $0.kind == "circle" }) { circle in
            if let box = projection.labels[circle.id] {
              Button { select(circle) } label: { Color.clear.contentShape(Rectangle()) }
                .buttonStyle(.plain).focusEffectDisabled().frame(width: box.width, height: box.height)
                .focused($labelFocus, equals: circle.id)
                .overlay { RoundedRectangle(cornerRadius: 4).stroke((labelFocus == circle.id || (snapshotFocus == "circle-label" && activeCircleID == circle.id)) ? Color.accentColor : .clear, lineWidth: 3) }
                .accessibilityLabel(circlePreview(circle.id).spoken)
                .accessibilityHint("Open this circle on the map")
                .offset(x: box.minX, y: box.minY)
            }
          }
          if let id = activeCircleID, let box = projection.labels[id] {
            let preview = circlePreview(id)
            CircleHoverCard(preview: preview)
              .frame(width: 208)
              .offset(x: CircleHoverPreview.cardOrigin(label: box, canvas: size, height: preview.cardHeight).x,
                      y: CircleHoverPreview.cardOrigin(label: box, canvas: size, height: preview.cardHeight).y)
              .allowsHitTesting(false)
          }
          if let progress, let event = replayEvent(progress) {
            Text(Timestamp.date(event.at)?.formatted(.dateTime.weekday(.abbreviated).day().month(.abbreviated)) ?? "")
              .font(.caption.weight(.medium)).foregroundStyle(AccessibleSecondary())
              .padding(10)
          }
          if (focus != nil || progress != nil), let event = callout(progress: progress, now: timeline.date),
            let point = eventPoint(event)
          {
            Text(event.capsule(nodes: map.nodes)).font(.caption2.weight(.medium)).lineLimit(1)
              .padding(.horizontal, 8).padding(.vertical, 5)
              .background(
                snapshot
                  ? AnyShapeStyle(Color(nsColor: .windowBackgroundColor))
                  : AnyShapeStyle(.regularMaterial), in: Capsule()
              )
              .overlay(Capsule().stroke(.secondary.opacity(0.15), lineWidth: 0.5))
              .frame(maxWidth: min(330, max(100, size.width - 24)), alignment: .leading)
              .offset(
                x: min(max(12, point.x - 120), max(12, size.width - 342)),
                y: point.y > 60 ? point.y - 48 : point.y + 34)
          }
          if activeCircleID == nil, focus != nil, let active, let text = tooltip(active) {
            Text(text).font(.caption).lineSpacing(3)
              .padding(10).frame(maxWidth: min(310, max(140, size.width - 24)), alignment: .leading)
              .background(
                snapshot
                  ? AnyShapeStyle(Color(nsColor: .windowBackgroundColor))
                  : AnyShapeStyle(.regularMaterial), in: RoundedRectangle(cornerRadius: 9)
              )
              .overlay(
                RoundedRectangle(cornerRadius: 9).stroke(.secondary.opacity(0.2), lineWidth: 0.5)
              )
              .offset(
                x: snapshotFrame == "hover"
                  ? 12 : min(max(12, pointer.x + 14), max(12, size.width - 334)),
                y: snapshotFrame == "hover" ? 92 : min(max(8, pointer.y + 14), 125))
          }
        }
        .onChange(of: elapsed.map { $0 >= 8 } ?? false) { _, done in
          if done { replayStart = nil }
        }
      }
  }
  private var mapGeometry: some View {
    mapDrawing
      .frame(height: focus == nil ? layout.overviewContentHeight : max(460, Double(map.nodes.count) * 11))
      .contentShape(Rectangle())
      .onGeometryChange(for: CGSize.self) {
        $0.size
      } action: { newSize in
        size = newSize
        store.updateUnderstandingLayout(map: map, size: newSize, focused: focus != nil)
      }
      .onChange(of: map.nodes) { _, _ in store.updateUnderstandingLayout(map: map, size: size, focused: focus != nil) }
      .onChange(of: map.edges) { _, _ in store.updateUnderstandingLayout(map: map, size: size, focused: focus != nil) }
      .onChange(of: hover) { _, hit in updateHover(hit) }
      .task(id: hoveredPersonID) { await loadHoverDetail() }
  }
  private var mapKeyboard: some View {
    mapGeometry
      .focusable().focused($mapHasFocus).focusEffectDisabled()
      .overlay {
        RoundedRectangle(cornerRadius: 12).strokeBorder(showsMapFocus ? Color.accentColor : increasedContrast ? .primary : .clear, lineWidth: showsMapFocus ? 3 : 1)
          .allowsHitTesting(false).accessibilityHidden(true)
      }
      .onKeyPress(keys: [.upArrow, .downArrow, .leftArrow, .rightArrow]) { press in
        labelFocus = nil; mapHasFocus = true
        keyboardNode.move(press.key == .upArrow || press.key == .leftArrow ? -1 : 1,
                          in: map.nodes.filter { $0.kind == "circle" || $0.kind == "person" }.map(\.id))
        if let id = keyboardNode.id { hover = .node(id) }
        return .handled
      }
      .onKeyPress(keys: [.return, .space]) { _ in
        if let id = labelFocus ?? keyboardNode.id, let node = map.nodes.first(where: { $0.id == id }) {
          select(node); return .handled
        }
        return .ignored
      }
      .onExitCommand { labelFocus = nil; hover = nil; keyboardNode.id = nil; store.mapSelection = MapSelection(); store.route(.escape) }
      .onChange(of: reduceMotion) { _, value in if value { replayStart = nil } }
      .task(id: focus.map { $0 + (store.circleMap.cursor ?? "") }) {
        focusedMap = nil; focusError = nil
        guard let focus else { return }
        do {
          var query = URLComponents(); query.queryItems = [URLQueryItem(name: "focus", value: focus)]
          let value: CircleMap = try Wire.decode(CircleMap.self, from: await MapLoadRetry.request(store.backend, path: "/v1/map?" + (query.percentEncodedQuery ?? "")))
          guard !Task.isCancelled, store.mapSelection.circle == focus else { return }
          focusedMap = value
        } catch { if !Task.isCancelled { focusError = "This circle couldn't load. Select it again to retry." } }
      }
  }
  private var mapSurface: some View {
    VStack(spacing: 0) {
      HStack {
        Spacer()
        Button {
          if !reduceMotion { replayStart = Date() }
        } label: {
          Label(replayStart == nil ? "Replay" : "Replaying", systemImage: "arrow.counterclockwise")
        }.font(.caption).limmrButton().foregroundStyle(AccessibleSecondary())
          .disabled(reduceMotion || events.isEmpty || replayStart != nil)
          .accessibilityValue(reduceMotion ? "Unavailable with Reduce Motion" : "Last 14 days")
          .help("Replay the last 14 days of understanding")
      }.padding(.horizontal, 16).padding(.top, 12)
      mapKeyboard
        .onContinuousHover { phase in updatePointer(phase) }
        .onTapGesture { (point: CGPoint) in tapMap(point) }
        .accessibilityLabel("People and understanding")
        .accessibilityValue(spokenMapSelection)
        .accessibilityHint("Use arrow keys to explore dots and circle labels, then Return to activate. Tab continues to the lists.")
      HStack(spacing: 6) {
        if let hoverCaption {
          Text(hoverCaption).lineLimit(1)
        } else {
          Circle().frame(width: 8, height: 8)
          Text("Recent")
          Circle().frame(width: 4, height: 4).padding(.leading, 8)
          Text("Older")
        }
        Spacer(minLength: 0)
      }.font(.caption2).foregroundStyle(AccessibleTertiary())
        .frame(height: 20).padding(.horizontal, 16).padding(.bottom, 10)
      Divider()
      HStack(spacing: 0) {
        ForEach(Array(summaryMetrics.enumerated()), id: \.offset) { index, metric in
          if index > 0 { Divider() }
          PeopleMapCountLabel(value: metric.1, title: metric.0)
            .padding(16).frame(maxWidth: .infinity, alignment: .leading)
        }
      }.fixedSize(horizontal: false, vertical: true)
    }
    .background(.ultraThinMaterial, in: RoundedRectangle(cornerRadius: 12))
    .overlay(RoundedRectangle(cornerRadius: 12).stroke(increasedContrast ? Color.primary : Color.secondary.opacity(0.12), lineWidth: 0.5))
    .clipShape(RoundedRectangle(cornerRadius: 12))
  }
  private var activeCircleID: String? {
    if snapshotFrame == "circle-hover" || snapshotFocus == "circle-label" {
      return (map.nodes.first { $0.kind == "circle" && $0.id == "family" }
        ?? map.nodes.first { $0.kind == "circle" })?.id
    }
    if let labelFocus { return labelFocus }
    let id: String?
    if case .node(let hovered) = hover { id = hovered }
    else { id = showsMapFocus ? keyboardFocusNodeID : nil }
    return map.nodes.first { $0.id == id && $0.kind == "circle" }?.id
  }
  private func circlePreview(_ id: String) -> CircleHoverPreview {
    CircleHoverPreview(id: id, circles: store.circles, people: store.people, map: focusedMap ?? store.circleMap)
  }
  private var spokenMapSelection: String {
    let selected = map.nodes.first { $0.id == keyboardNode.id }
    if let id = activeCircleID { return circlePreview(id).spoken }
    return Spoken.map(map) + (selected.map { ". Selected \(Spoken.node($0))" } ?? "")
  }
  private func mapHit(_ point: CGPoint) -> MapHit? {
    layout.hit(point, map: map)
  }
  private func updatePointer(_ phase: HoverPhase) {
    switch phase {
    case .active(let point): pointer = point; hover = mapHit(point)
    case .ended: hover = nil
    }
  }
  private func tapMap(_ point: CGPoint) {
    mapHasFocus = true
    switch mapHit(point) {
    case .node(let id): select(map.nodes.first { $0.id == id })
    case nil: select(nil)
    }
  }
  private func select(_ node: CircleMapNode?) {
    if let open = store.mapSelection.click(node) { store.circlePath.append(.person(open)) }
    if node?.kind == "person", let id = node?.id {
      store.selectedPerson = id
      Task { await store.loadPerson() }
    }
  }
  private var hoveredPersonID: String? {
    guard case .node(let id) = hover, map.nodes.contains(where: { $0.id == id && $0.kind == "person" }) else { return nil }
    return id
  }
  private func loadHoverDetail() async {
    hoverDetail = nil
    guard focus != nil, !snapshot, let id = hoveredPersonID,
      let encoded = id.addingPercentEncoding(withAllowedCharacters: .alphanumerics) else { return }
    let generation = store.backendGeneration
    do {
      try await Task.sleep(for: .milliseconds(250))
      let detail: PersonDetail = try await store.backend.get("/v1/people/" + encoded)
      guard !Task.isCancelled, hoveredPersonID == id, store.backendGeneration == generation else { return }
      hoverDetail = detail
    } catch { /* The bounded map card remains usable if detail is unavailable. */ }
  }
  private func updateHover(_ hit: MapHit?) {
    guard focus != nil else { return }
    var person = store.mapSelection.person
    if case .node(let value) = hit { person = value }
    store.updateUnderstandingLayout(map: map, size: size, focused: focus != nil, extra: person)
  }
  private func prepareMap() {
    preparedMap = UnderstandingPresentation.calmMap(focusedMap ?? store.circleMap, selected: store.mapSelection.person, expanded: expanded, focus: focus)
    store.updateUnderstandingLayout(map: preparedMap, size: size, focused: focus != nil)
  }
  private var hoverCaption: String? {
    let id = snapshotFrame == "hover" ? snapshotHoverID : hoveredPersonID
    guard let id, let node = map.nodes.first(where: { $0.id == id && $0.kind == "person" }) else { return nil }
    let circle = UnderstandingLayout.primary(node, map: map).flatMap { id in map.nodes.first { $0.id == id }?.title }
    return node.title + (circle.map { " · " + $0 } ?? " · Not in a circle yet")
  }
  private func draw(
    context: inout GraphicsContext, layout: UnderstandingLayout, time: Double, now: Date, progress: Double?, active: MapHit?
  ) {
    let map = self.map
    var neighbours: Set<String> = []
    if case .node(let id) = active {
      neighbours.insert(id)
      for edge in map.edges
      where edge.state != "reverted" && (edge.source == id || edge.target == id) {
        neighbours.insert(edge.source)
        neighbours.insert(edge.target)
      }
    }
    // Chats stay in the focused detail; hovering an overview circle reveals only its people.
    let chats = Set(map.nodes.filter { $0.kind == "chat" }.map(\.id))
    let focus = neighbours.union([store.mapSelection.circle].compactMap { $0 })
    let emptyCircles = Set(map.nodes.filter { n in n.kind == "circle" && !map.nodes.contains { $0.kind == "person" && UnderstandingLayout.memberships($0, map: map).contains(n.id) } }.map(\.id))
    let shown = { (id: String) in
      !chats.contains(id) || (self.focus != nil && (focus.contains(id) || map.edges.contains { $0.target == id && emptyCircles.contains($0.source) }))
    }
    var keyboardBounds: CGRect?
    for node in map.nodes.sorted(by: { ($0.kind == "circle" ? 0 : 1) < ($1.kind == "circle" ? 0 : 1) }) where shown(node.id) {
      guard let point = layout.displayed(node.id, time: time) else { continue }
      let dim = neighbours.isEmpty || neighbours.contains(node.id) ? 1.0 : 0.16
      let highlighted = active == .node(node.id) || store.mapSelection.person == node.id || (activeCircleID != nil && UnderstandingLayout.memberships(node, map: map).contains(activeCircleID!))
      let selected = store.mapSelection.person == node.id || store.mapSelection.circle == node.id
      let emphasis = layout.driftEmphasis(node, map: map)
      let recent = layout.recency[node.id] ?? 0
      let tier = recent > 0.75 ? 5.0 : recent > 0.3 ? 3.8 : 2.7
      let radius = self.focus == nil ? tier * (0.65 + emphasis * 0.35) : UnderstandingLayout.radius(node)
      let rect = CGRect(x: point.x-radius, y: point.y-radius, width: radius*2, height: radius*2)
      let shape = node.kind == "chat" ? Path(rect) : Path(ellipseIn: rect)
      // Circle anchors are labels, not hub discs. The field remains one layer of people.
      if self.focus != nil || node.kind != "circle" {
        let opacity = self.focus == nil
          ? (UnderstandingLayout.primary(node, map: map) == nil ? 0.07 + recent * 0.03 : 0.10 + emphasis * (0.18 + recent * 0.32))
          : (node.kind == "circle" ? 0.12 : 0.36)
        if node.kind == "person", let avatar = context.resolveSymbol(id: node.id) {
          var symbolContext = context; symbolContext.opacity = dim
          symbolContext.draw(avatar, in: rect)
        } else {
        context.fill(shape, with: .color((highlighted ? Color.accentColor : .primary).opacity(highlighted ? 0.9 : increasedContrast ? max(0.7, opacity * dim) : opacity * dim)))
        }
        if selected && !(showsMapFocus && keyboardFocusNodeID == node.id) {
          context.stroke(Path(ellipseIn: rect.insetBy(dx: -4, dy: -4)), with: .color(.accentColor), lineWidth: 2)
        }
      }
      if showsMapFocus && keyboardFocusNodeID == node.id {
        keyboardBounds = rect.union(layout.labels[node.id] ?? rect).insetBy(dx: -8, dy: -6)
      }
      guard let label = layout.labels[node.id], let text = layout.labelText[node.id] else { continue }
      let labelPoint = CGPoint(x: label.midX, y: label.midY)
      context.fill(Path(roundedRect: label, cornerRadius: 3), with: .color(Color(nsColor: .windowBackgroundColor).opacity(0.92)))
      context.draw(Text(text).font(.system(size: node.kind == "circle" ? 11 : 10, weight: .medium)).foregroundColor(.primary.opacity(increasedContrast ? 1 : node.kind == "circle" ? 0.50 : max(0.5, dim))), at: labelPoint)

    }
    // Draw once after every node and label, including dots without labels.
    // Neighbouring label backings must not cut through the keyboard ring.
    if let keyboardBounds {
      context.stroke(Path(roundedRect: keyboardBounds, cornerRadius: 12), with: .color(.accentColor), lineWidth: 3)
    }
  }
  private var replayDuration: Double {
    max(
      1,
      (events.last.flatMap { Timestamp.date($0.at) } ?? Date()).timeIntervalSince(
        events.first.flatMap { Timestamp.date($0.at) } ?? Date()))
  }
  private func replayDate(_ progress: Double) -> Date? {
    events.first.flatMap { Timestamp.date($0.at) }?.addingTimeInterval(replayDuration * progress)
  }
  private func replayEvent(_ progress: Double) -> MapEvent? {
    guard let at = replayDate(progress) else { return nil }
    return events.last { (Timestamp.date($0.at) ?? .distantFuture) <= at }
  }
  private func callout(progress: Double?, now: Date) -> MapEvent? {
    if let progress { return replayEvent(progress) }
    return store.mapArrivals.first { now >= $0.at && now.timeIntervalSince($0.at) < 3 }?.event
  }
  private func eventPoint(_ event: MapEvent) -> CGPoint? {
    guard let edge = map.edges.first(where: { $0.key == event.edgeId }),
      let a = layout.positions[edge.source], let b = layout.positions[edge.target]
    else {
      return event.nodeIds.compactMap { layout.positions[$0] }.first
    }
    return CGPoint(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2)
  }
  private func tooltip(_ hit: MapHit) -> String? {
    switch hit {
    case .node(let id):
      guard let node = map.nodes.first(where: { $0.id == id }) else { return nil }
      var detail = node
      if let profile = hoverDetail, profile.person.id == id {
        let person = profile.person
        detail.howKnown = person.howKnown.isEmpty ? person.inferredRelationship?.howKnown : person.howKnown
        detail.circleAffinity = person.circleAffinity
        detail.groupsInCommon = person.groupsInCommon
        detail.groupsInCommonCount = person.groupsInCommon?.count ?? profile.groups.count
      }
      return UnderstandingPresentation.nodeText(
        detail, circles: store.circles,
        now: store.demo ? Timestamp.date(DemoData.now)! : Date())
    }
  }
}

private struct SnapshotMapFrameKey: EnvironmentKey { static let defaultValue: String? = nil }
extension EnvironmentValues {
  var snapshotMapFrame: String? {
    get { self[SnapshotMapFrameKey.self] }
    set { self[SnapshotMapFrameKey.self] = newValue }
  }
}
