import AppKit
import ImageIO
import SwiftUI
import LimmrKit

/// A single Canvas, with one real SwiftUI avatar reserved for the matched-geometry flight.
/// Layout and thumbnail decoding never run in the drawing closure.
struct NetworkMapView: View {
  @Bindable var store: AppStore
  let map: CircleMap
  var selected: String? = nil
  var replayEvent: MapEvent? = nil
  var compact = false
  var focusesSelection = true
  var namespace: Namespace.ID? = nil
  var onSelect: (CircleMapNode?) -> Void = { _ in }
  @Environment(\.colorScheme) private var scheme
  @Environment(\.accessibilityReduceMotion) private var systemReduceMotion
  @Environment(\.snapshotReducedMotion) private var snapshotReduceMotion
  @Environment(\.snapshotRendering) private var snapshot
  @Environment(\.snapshotFocusedControl) private var snapshotFocus
  @State private var scene = NetworkMapProjection()
  @State private var zoom = 1.0
  @State private var pan = CGSize.zero
  @GestureState private var drag = CGSize.zero
  @GestureState private var magnification = 1.0
  @State private var hover: String?
  @State private var keyboard = KeyboardSelection()
  @FocusState private var focused: Bool
  @State private var images: [String: Image] = [:]
  private var reduceMotion: Bool { systemReduceMotion || snapshotReduceMotion }
  private var dark: Bool { scheme == .dark }
  private var active: String? { hover ?? keyboard.id ?? selected ?? replayEvent?.nodeIds.first }
  private var performance: Bool { ProcessInfo.processInfo.arguments.contains("--n1-map-benchmark") }

  init(store: AppStore, map: CircleMap, selected: String? = nil, compact: Bool = false,
       namespace: Namespace.ID? = nil, focusesSelection: Bool = true, replayEvent: MapEvent? = nil, onSelect: @escaping (CircleMapNode?) -> Void = { _ in }) {
    self.store = store; self.map = map; self.selected = selected; self.compact = compact
    self.focusesSelection = focusesSelection
    self.namespace = namespace; self.onSelect = onSelect; self.replayEvent = replayEvent
    let cached = NetworkMapLayoutCache.cached(map) ?? NetworkMapProjection()
    _scene = State(initialValue: compact && selected != nil ? PersonNetworkPresentation.miniScene(cached, selected: selected!) : cached)
  }

  var body: some View {
    VStack(spacing: 0) {
      accessibleMap
      if !compact {
        HStack {
          Text("Drag to explore · Pinch to zoom").font(.caption2).foregroundStyle(AccessibleSecondary())
          Spacer(minLength: 8)
          zoomControls
        }.padding(.horizontal, 12).padding(.vertical, 8)
          .background(Color(nsColor: .controlBackgroundColor))
      }
    }.clipShape(RoundedRectangle(cornerRadius: 14))
      .overlay(RoundedRectangle(cornerRadius: 14).stroke(.primary.opacity(0.08), lineWidth: 1))
      .task(id: NetworkMapInput(nodes: map.nodes, edges: map.edges, generation: store.backendGeneration)) {
        await prepare()
      }
      .onChange(of: store.backendGeneration) { _, _ in images = [:] }
      .onChange(of: store.avatarData) { old, new in
        // Custom-photo edits and removals invalidate the rendered preview too.
        for (id, previous) in old where new[id] != previous { images[id] = nil }
        for (id, data) in new where old[id] != data && scene.nodesByID[id] != nil && images[id] == nil {
          if let image = NetworkAvatarThumbnail.decode(data) { images[id] = Image(decorative: image, scale: 2) }
        }
      }
  }
  private var accessibleMap: some View {
    GeometryReader { geometry in surface(size: geometry.size) }
      .background(Color(nsColor: .controlBackgroundColor))
      .clipShape(RoundedRectangle(cornerRadius: 14))
      .accessibilityElement(children: .ignore)
      .accessibilityLabel("People network map")
      .accessibilityValue(spokenSummary)
      .accessibilityHint("Use the People and Circles lists to navigate. Drag to pan and pinch to zoom.")
      .focusable(!compact).focusEffectDisabled().focused($focused)
      .onKeyPress(keys: [.leftArrow, .rightArrow, .upArrow, .downArrow]) { key in
        keyboard.move(key.key == .leftArrow || key.key == .upArrow ? -1 : 1, in: scene.nodes.filter { $0.kind != "chat" }.map(\.id))
        return .handled
      }
      .onKeyPress(keys: [.return, .space]) { _ in
        guard let id = keyboard.id else { return .ignored }; onSelect(scene.nodesByID[id]); return .handled
      }
  }
  private var spokenSummary: String {
    "\(scene.peopleCount) people, \(scene.circleCount) circles" + ((keyboard.id ?? selected).map { ", highlighting \(scene.nodesByID[$0]?.title ?? "person") and their connections" } ?? "")
  }
  private func surface(size: CGSize) -> some View {
    let camera = camera(for: size)
    let visible = scene.visible(camera: camera)
    let loadID = String(store.backendGeneration) + ":" + String(camera.showsAvatars) + ":" + visible.map(\.id).joined(separator: ",")
    return drawing(camera: camera, size: size)
      .contentShape(Rectangle()).clipped()
      .onContinuousHover { phase in
        guard !compact else { return }
        switch phase {
        case .active(let point): hover = scene.hit(point, camera: camera)
        case .ended: hover = nil
        }
      }
      .onTapGesture { (point: CGPoint) in
        guard !compact else { return }; focused = true
        onSelect(scene.hit(point, camera: camera).flatMap { scene.nodesByID[$0] })
      }
      .gesture(panGesture).simultaneousGesture(zoomGesture)
       .task(id: loadID) {
        await loadVisible(visible, camera: camera)
        guard !snapshot && !store.demo else { return }
        let generation = store.backendGeneration
        while !Task.isCancelled && generation == store.backendGeneration {
          do { try await Task.sleep(for: .seconds(15)) } catch { return }
          guard generation == store.backendGeneration else { return }
          for node in visible where node.kind == "person" && store.avatarData[node.id] == nil { store.avatarAttempts.remove(node.id) }
          await loadVisible(visible, camera: camera)
        }
      }
      .task(id: scene.revision + ":benchmark") {
        if performance && !compact && !scene.positions.isEmpty {
          await loadVisible(scene.nodes, camera: NetworkMapCamera(viewport: size, world: scene.world))
          NetworkMapBenchmark.run(scene: scene, images: images, circles: store.circles, size: size, dark: dark)
        }
      }
  }
  private var panGesture: some Gesture {
    DragGesture(minimumDistance: 4).updating($drag) { value, state, _ in
      if !compact { state = value.translation }
    }.onEnded { value in if !compact { pan.width += value.translation.width; pan.height += value.translation.height } }
  }
  private var zoomGesture: some Gesture {
    MagnifyGesture().updating($magnification) { value, state, _ in
      if !compact { state = value.magnification }
    }.onEnded { value in if !compact { zoom = min(4, max(0.45, zoom * value.magnification)) } }
  }
  private var zoomControls: some View {
    HStack(spacing: 12) {
      Button { setZoom(zoom / 1.25) } label: { Image(systemName: "minus.magnifyingglass") }.help("Zoom out").accessibilityLabel("Zoom out")
      Button(selected == nil ? "Fit" : "Recenter") { zoom = 1; pan = .zero }.accessibilityLabel(selected == nil ? "Fit network" : "Recenter on highlighted person")
      Button { setZoom(zoom * 1.25) } label: { Image(systemName: "plus.magnifyingglass") }.help("Zoom in").accessibilityLabel("Zoom in")
    }.buttonStyle(.plain).font(.callout).padding(.horizontal, 10).padding(.vertical, 6)
      .background(.regularMaterial, in: Capsule())
  }
  private func drawing(camera: NetworkMapCamera, size: CGSize) -> some View {
      ZStack(alignment: .topLeading) {
        Canvas { context, _ in
          NetworkMapRenderer.draw(context: &context, scene: scene, camera: camera, images: images,
            circles: store.circles, dark: dark, active: selected ?? active, selected: selected,
            omitSelected: namespace != nil && selected != nil, compact: compact)
        }.accessibilityHidden(true)
        if namespace != nil, let selected, let node = scene.nodesByID[selected], let world = scene.positions[selected] {
          let point = camera.screen(world)
          if camera.isVisible(world, radius: 32) {
            flightAvatar(node, diameter: compact ? 32 : 44)
              .position(point).allowsHitTesting(false).accessibilityHidden(true)
          }
        }
        if !compact, let id = active, let node = scene.nodesByID[id], let p = scene.positions[id] {
          let point = camera.screen(p)
          if camera.isVisible(p, radius: 0) {
            Text(node.title).font(.system(size: 12, weight: .semibold)).lineLimit(1)
              .padding(.horizontal, 10).padding(.vertical, 6)
              .background(Color(nsColor: .controlBackgroundColor), in: Capsule())
              .overlay(Capsule().stroke(.primary.opacity(0.12)))
              .position(x: min(max(90, point.x), max(90, size.width - 90)),
                        y: min(max(16, point.y + 40), size.height - 16))
              .allowsHitTesting(false).accessibilityHidden(true)
          }
        }
        if scene.positions.isEmpty {
          Text(map.nodes.isEmpty ? "Your map appears as circles sync." : "Arranging your map…")
            .font(.caption).foregroundStyle(AccessibleSecondary()).padding(24)
        }
      }
  }
  private func prepare() async {
    guard scene.map.nodes != map.nodes || scene.map.edges != map.edges else { return }
    let value = await NetworkMapLayoutCache.projection(map)
    guard !Task.isCancelled else { return }
    scene = compact && selected != nil ? PersonNetworkPresentation.miniScene(value, selected: selected!) : value
  }
  private func camera(for size: CGSize) -> NetworkMapCamera {
    let offset = CGSize(width: pan.width + drag.width, height: pan.height + drag.height)
    let factor = zoom * min(4, max(0.45, magnification))
    if focusesSelection, let selected {
      return scene.focusedCamera(viewport: size, selected: selected, zoom: factor, pan: offset,
        padding: compact ? CGSize(width: 28, height: 32) : CGSize(width: 48, height: 56))
    }
    return NetworkMapCamera(viewport: size, world: scene.world, zoom: factor,
      pan: offset, focus: nil)
  }
  @ViewBuilder private func flightAvatar(_ node: CircleMapNode, diameter: Double) -> some View {
    let avatar = Group {
      if let image = images[node.id] { image.resizable().scaledToFill() }
      else { Circle().fill(.secondary.opacity(0.6)).padding(diameter * 0.3) }
    }.frame(width: diameter, height: diameter).clipShape(Circle())
      .overlay(Circle().stroke(.primary.opacity(0.65), lineWidth: 1).padding(-3))
    if let namespace, !reduceMotion {
      avatar.matchedGeometryEffect(id: "network-avatar-" + node.id, in: namespace)
    } else { avatar }
  }
  private func setZoom(_ value: Double) {
    // Camera changes remain directly manipulable; only the profile flight animates geometry.
    zoom = min(4, max(0.45, value))
  }
  private func loadVisible(_ nodes: [CircleMapNode], camera: NetworkMapCamera) async {
    let generation = store.backendGeneration
    var batch: [String: Image] = [:]
    var published = Date()
    for (index, node) in nodes.enumerated() where node.kind == "person" && (camera.showsAvatars || scene.peopleCount <= 120) {
      guard !Task.isCancelled, generation == store.backendGeneration else { return }
      if images[node.id] != nil { continue }
      await store.loadAvatar(node.id)
      guard !Task.isCancelled, generation == store.backendGeneration else { return }
      if let data = store.avatarData[node.id], let image = NetworkAvatarThumbnail.decode(data) {
        batch[node.id] = Image(decorative: image, scale: 2)
      }
      // Cached fixtures can decode hundreds immediately. Publish in batches instead of
      // rebuilding the Canvas for every thumbnail; slow network responses still publish promptly.
      if batch.count >= 16 || Date().timeIntervalSince(published) >= 0.05 {
        images.merge(batch) { _, new in new }; batch.removeAll(); published = Date()
        await Task.yield()
      } else if index % 16 == 0 { await Task.yield() }
    }
    guard !Task.isCancelled, generation == store.backendGeneration else { return }
    images.merge(batch) { _, new in new }
  }
}

enum NetworkAvatarThumbnail {
  /// ImageIO decodes a bounded 96 px preview, never a full backend photo during pan/zoom.
  static func decode(_ data: Data) -> CGImage? {
    guard data.count <= 5_000_000, let source = CGImageSourceCreateWithData(data as CFData, nil) else { return nil }
    return CGImageSourceCreateThumbnailAtIndex(source, 0, [
      kCGImageSourceCreateThumbnailFromImageAlways: true,
      kCGImageSourceCreateThumbnailWithTransform: true,
      kCGImageSourceThumbnailMaxPixelSize: 96,
      kCGImageSourceShouldCacheImmediately: true,
    ] as CFDictionary)
  }
}

@MainActor enum NetworkMapRenderer {
  static func draw(context: inout GraphicsContext, scene: NetworkMapProjection, camera: NetworkMapCamera,
                   images: [String: Image], circles: [LimmrKit.Circle], dark: Bool, active: String?, selected: String?,
                   omitSelected: Bool = false, compact: Bool = false) {
    let related = scene.neighbours(of: active)
    for edge in scene.edges {
      guard let a = scene.positions[edge.source], let b = scene.positions[edge.target] else { continue }
      let linked = active == edge.source || active == edge.target
      // Resting edges stay quiet; only direct links are emphasized on selection/hover.
      let style = edge.style
      var path = Path(); path.move(to: camera.screen(a)); path.addLine(to: camera.screen(b))
      context.stroke(path, with: .color(Color.primary.opacity(active == nil ? style.opacity * 0.40 : linked ? 0.52 : 0.035)),
        style: StrokeStyle(lineWidth: linked ? 1.3 : max(0.4, style.width), dash: style.dashed ? [3, 4] : []))
    }
    let dense = scene.peopleCount > 80
    let circleLabels = NetworkCircleLabels.frames(scene: scene, camera: camera)
    let showsPhotos = camera.showsAvatars || scene.peopleCount <= 120
    let visible = scene.visible(camera: camera)
    // Draw every avatar first, then hubs, then the label plates above the whole field.
    for node in visible.sorted(by: { ($0.kind == "circle" ? 1 : 0) < ($1.kind == "circle" ? 1 : 0) }) {
      guard let world = scene.positions[node.id] else { continue }
      if omitSelected && node.id == selected { continue }
      let point = camera.screen(world)
      var layer = context
      layer.opacity = node.kind == "circle" || active == nil || related.contains(node.id) ? 1 : 0.4
      let diameter = node.kind == "circle" ? 0.0 : node.kind == "chat" ? 4.0 : showsPhotos && images[node.id] != nil ? (compact ? 16.0 : dense ? min(14, max(7, camera.scale * 34)) : 14.0) : 5.0
      let rect = CGRect(x: point.x - diameter / 2, y: point.y - diameter / 2, width: diameter, height: diameter)
      if node.kind == "circle" {
        // Circle anchors are quiet labels, never coloured discs.
        if let box = circleLabels[node.id] {
          let title = Text(node.title).font(.system(size: 11, weight: .medium)).foregroundColor(.secondary)
          layer.fill(Path(roundedRect: box, cornerRadius: 4), with: .color(Color(nsColor: .controlBackgroundColor)))
          layer.draw(layer.resolve(title), in: box.insetBy(dx: 4, dy: 2))
        }
      } else if node.kind == "chat" {
        layer.fill(Path(rect), with: .color(.secondary))
      } else if showsPhotos, let image = images[node.id] {
        var photo = layer; photo.clip(to: Path(ellipseIn: rect)); photo.draw(image, in: rect)
        layer.stroke(Path(ellipseIn: rect), with: .color(.primary.opacity(0.14)), lineWidth: 0.5)
      } else {
        layer.fill(Path(ellipseIn: rect), with: .color(.primary.opacity(0.5)))
      }
      if node.id == active && node.kind != "circle" {
        layer.stroke(Path(ellipseIn: rect.insetBy(dx: -4, dy: -4)), with: .color(.primary.opacity(0.6)), lineWidth: 1.5)
      }
    }

  }
}

private struct NetworkMapInput: Equatable {
  var nodes: [CircleMapNode]
  var edges: [CircleMapEdge]
  var generation: Int
}

/// Preparation runs off the UI actor and is shared by overlapping mini/full map requests.
/// A prepared mini map gives the flight destination its positions on the very first render.
@MainActor enum NetworkMapLayoutCache {
  struct Revision: Equatable {
    let nodes: [CircleMapNode]
    let edges: [CircleMapEdge]
    init(_ map: CircleMap) {
      nodes = map.nodes.sorted { $0.id < $1.id }
      edges = map.edges.sorted { $0.key < $1.key }
    }
  }
  private(set) static var preparations = 0
  static var entries: [(Revision, NetworkMapProjection)] = []
  static var pending: [(Revision, Task<NetworkMapProjection, Never>)] = []
  static func cached(_ map: CircleMap) -> NetworkMapProjection? {
    entries.first(where: { $0.0 == Revision(map) })?.1
  }
  static func projection(_ map: CircleMap) async -> NetworkMapProjection {
    let revision = Revision(map)
    if let index = entries.firstIndex(where: { $0.0 == revision }) {
      let hit = entries.remove(at: index); entries.append(hit)
      return hit.1
    }
    let task: Task<NetworkMapProjection, Never>
    if let current = pending.first(where: { $0.0 == revision }) {
      task = current.1
    } else {
      preparations += 1
      task = Task.detached(priority: .userInitiated) {
        let start = Date()
        var layout = UnderstandingLayout()
        layout.updateNetwork(map: map, size: CGSize(width: 1600, height: 1000))
        var result = NetworkMapProjection(map: map, positions: layout.positions)
        result.preparationMilliseconds = Date().timeIntervalSince(start) * 1000
        return result
      }
      pending.append((revision, task))
    }
    let result = await task.value
    pending.removeAll { $0.0 == revision }
    if cached(map) == nil {
      entries.append((revision, result)); if entries.count > 4 { entries.removeFirst() }
    }
    return result
  }
}
