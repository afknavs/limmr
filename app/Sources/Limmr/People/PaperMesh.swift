import Foundation
import LimmrKit
import SwiftUI

/// Prepared off the UI actor. No RNG state, display clock, image loading or model work.
enum PaperMesh {
  static let referenceScale = 1.0
  static func prepare(_ original: CircleMap, size: CGSize = CGSize(width: 712, height: 430)) -> NetworkMapProjection {
    let layout = PaperIslandLayout.prepare(original, size: size)
    var map = original
    if !map.nodes.contains(where: { $0.id == "me" }) { map.nodes.append(CircleMapNode(id: "me", kind: "person", title: "You")) }
    var points = layout.positions
    for (id,point) in layout.labels { points[id] = point }
    var scene = NetworkMapProjection(map: map, positions: points)
    scene.map = original; scene.world = size; scene.isPaperMesh = true
    scene.meshDiameters = layout.diameters; scene.meshHubs = layout.hubs
    scene.meshFaces = layout.faces; scene.meshCounts = layout.counts; scene.meshMemberships = layout.memberships
    scene.edges = layout.edges; scene.meshRestingEdges = layout.net
    scene.meshLastSpoke = Dictionary(uniqueKeysWithValues: map.nodes.compactMap { person in (person.lastContact ?? person.lastActiveAt).flatMap(Timestamp.date).map { (person.id, $0.timeIntervalSince1970) } })
    for edge in layout.edges { scene.meshAdjacent[edge.source, default: []].append(edge); scene.meshAdjacent[edge.target, default: []].append(edge) }
    scene.meshLabelFrames = layout.labelFrames
    return scene
  }
  /// Shared-chat count is the edge strength; closeness and IDs break ties stably.
  static func selectedEdges(_ scene: NetworkMapProjection, person: String, faded: Set<String> = []) -> [CircleMapEdge] {
    func groups(_ id: String) -> Set<String> {
      Set(scene.nodesByID[id]?.sharedGroupIds ?? scene.nodesByID[id]?.groupsInCommon?.map(\.id) ?? [])
    }
    let own = groups(person)
    func other(_ edge: CircleMapEdge) -> String { edge.source == person ? edge.target : edge.source }
    return Array(scene.meshAdjacent[person, default: []].filter {
      !faded.contains($0.source) && !faded.contains($0.target)
    }.sorted {
      let a = other($0), b = other($1)
      let sa = $0.strength ?? Double(own.intersection(groups(a)).count)
      let sb = $1.strength ?? Double(own.intersection(groups(b)).count)
      if sa != sb { return sa > sb }
      let ca = scene.nodesByID[a]?.closeness ?? 0, cb = scene.nodesByID[b]?.closeness ?? 0
      return ca == cb ? a < b : ca > cb
    }.prefix(8))
  }
  static func newPeople(_ map: CircleMap, now: Date) -> Set<String> {
    Set(map.nodes.filter { $0.newThisWeek == true }.map(\.id)).union(Set(map.events.filter { ["person_added", "node_added"].contains($0.kind) && Timestamp.date($0.at).map { $0 <= now && now.timeIntervalSince($0) <= 7 * 86400 } == true }.flatMap(\.nodeIds)))
  }
}

@MainActor enum PaperMeshRenderer {
  static func draw(context: inout GraphicsContext, scene: NetworkMapProjection, camera: NetworkMapCamera, images: [String: Image], selected: String?, needs: Set<String> = [], newPeople: Set<String> = [], now: Date = Date(), islandImage: Image? = nil, hover: String? = nil, faded: Set<String> = []) {
    if let owner = scene.positions["me"] {
      let a = camera.screen(owner)
      for id in scene.meshHubs.keys.sorted() {
        let closest = scene.nodes.filter { scene.meshMemberships[$0.id, default: []].contains(id) }.max { ($0.closeness ?? $0.weight ?? 0) < ($1.closeness ?? $1.weight ?? 0) }
        guard let p = closest.flatMap({ scene.positions[$0.id] }) else { continue }
        let b = camera.screen(p)
        var curve = Path(); curve.move(to:a); curve.addQuadCurve(to:b,control:CGPoint(x:(a.x+b.x)/2+(b.y-a.y)*0.14,y:(a.y+b.y)/2-(b.x-a.x)*0.14))
        context.stroke(curve,with:.color(LimmrTheme.ink.opacity(0.12)),style:StrokeStyle(lineWidth:0.8,lineCap:.round))
      }
    }
    if let islandImage {
      let origin = camera.screen(.zero)
      context.draw(islandImage,in:CGRect(x:origin.x,y:origin.y,width:scene.world.width*camera.scale,height:scene.world.height*camera.scale))
    }
    var quiet = Path(), highlighted = Path(), hovered = Path()
    for edge in scene.meshRestingEdges where selected != edge.source && selected != edge.target && !faded.contains(edge.source) && !faded.contains(edge.target) {
      guard let a = scene.positions[edge.source], let b = scene.positions[edge.target] else { continue }
      if hover == edge.source || hover == edge.target { hovered.move(to:camera.screen(a)); hovered.addLine(to:camera.screen(b)) }
      else { quiet.move(to: camera.screen(a)); quiet.addLine(to: camera.screen(b)) }
    }
    for edge in selected.map({ PaperMesh.selectedEdges(scene, person: $0, faded: faded) }) ?? [] {
      guard let a = scene.positions[edge.source], let b = scene.positions[edge.target] else { continue }
      let s = camera.screen(a), e = camera.screen(b)
      // A slight bow keeps a selected person's links from reading as hard spokes.
      highlighted.move(to: s); highlighted.addQuadCurve(to: e, control: CGPoint(x:(s.x+e.x)/2+(e.y-s.y)*0.12, y:(s.y+e.y)/2-(e.x-s.x)*0.12))
    }
    context.stroke(quiet,with:.color(LimmrTheme.ink.opacity(0.12)),style:StrokeStyle(lineWidth:0.8,lineCap:.round))
    context.stroke(hovered,with:.color(LimmrTheme.ink.opacity(0.28)),style:StrokeStyle(lineWidth:0.9,lineCap:.round))
    context.stroke(highlighted,with:.color(LimmrTheme.ink.opacity(0.3)),style:StrokeStyle(lineWidth:1,lineCap:.round))
    if let selected, let p = scene.positions[selected], let owner = scene.positions["me"], scene.nodesByID[selected]?.kind == "person" {
      let a = camera.screen(owner), b = camera.screen(p)
      var curve = Path(); curve.move(to:a); curve.addQuadCurve(to:b,control:CGPoint(x:(a.x+b.x)/2+(b.y-a.y)*0.1,y:(a.y+b.y)/2-(b.x-a.x)*0.1))
      context.stroke(curve,with:.color(LimmrTheme.ink.opacity(0.34)),style:StrokeStyle(lineWidth:1.1,lineCap:.round))
    }
    let nodes = scene.visible(camera:camera).filter { $0.kind == "person" }.sorted {
      let a = scene.meshFaces.contains($0.id) || $0.id == selected, b = scene.meshFaces.contains($1.id) || $1.id == selected
      return a == b ? $0.id < $1.id : !a
    }
    for node in nodes {
      guard let point = scene.positions[node.id].map(camera.screen) else { continue }
      let face = scene.meshFaces.contains(node.id) || node.id == selected || needs.contains(node.id)
      var diameter = scene.meshDiameters[node.id, default: 6]*camera.scale
      if node.id == selected { diameter = max(30,diameter) }
      if needs.contains(node.id) { diameter = max(26,diameter) }
      if hover == node.id { diameter *= 1.15 }
      if newPeople.contains(node.id), !face { diameter += 1.2 }
      let rect = CGRect(x:point.x-diameter/2,y:point.y-diameter/2,width:diameter,height:diameter)
      let background = scene.meshMemberships[node.id, default: []].isEmpty ? LimmrTheme.panel : LimmrTheme.surface
      var layer = context; layer.opacity = faded.contains(node.id) ? 0.08 : 1
      if face {
        layer.fill(Path(ellipseIn:rect),with:.color(LimmrTheme.surface2))
        if let image = images[node.id] { var photo = layer; photo.clip(to:Path(ellipseIn:rect)); photo.draw(image,in:rect) }
        else { layer.draw(Text(PersonAvatar.initials(node.title)).font(LimmrTheme.font(12,.medium)).foregroundColor(LimmrTheme.ink2),at:point) }
        layer.stroke(Path(ellipseIn:rect.insetBy(dx:-1,dy:-1)),with:.color(background),lineWidth:2)
        if newPeople.contains(node.id) { layer.stroke(Path(ellipseIn:rect.insetBy(dx:-2,dy:-2)),with:.color(LimmrTheme.info),lineWidth:2) }
      } else {
        let days = scene.meshLastSpoke[node.id].map { max(0,now.timeIntervalSince1970-$0)/86400 } ?? 90
        let recency = max(0,1-days/90), opacity = 0.2+0.6*recency*recency
        layer.fill(Path(ellipseIn:rect.insetBy(dx:-1.5,dy:-1.5)),with:.color(background))
        layer.fill(Path(ellipseIn:rect),with:.color(newPeople.contains(node.id) ? LimmrTheme.info : LimmrTheme.ink.opacity(opacity)))
      }
      if node.id == selected { layer.stroke(Path(ellipseIn:rect.insetBy(dx:-3,dy:-3)),with:.color(LimmrTheme.ink),lineWidth:2) }
      if needs.contains(node.id) || node.needsYou == true { layer.fill(Path(roundedRect:CGRect(x:rect.maxX-6,y:rect.minY-3,width:9,height:9),cornerRadius:2.5),with:.color(LimmrTheme.brand)) }
      if node.id == "me" { layer.draw(Text("You").font(LimmrTheme.font(12,.semibold)).foregroundColor(LimmrTheme.ink),at:CGPoint(x:point.x,y:rect.maxY+12)) }
    }
    for (id,box) in NetworkCircleLabels.frames(scene:scene,camera:camera) {
      guard let node = scene.nodesByID[id] else { continue }
      context.fill(Path(roundedRect:box,cornerRadius:6),with:.color(LimmrTheme.panel))
      context.draw(Text(node.title).font(LimmrTheme.font(14,.semibold)).foregroundColor(LimmrTheme.ink),at:CGPoint(x:box.midX,y:box.minY+10))
      context.draw(Text("\(scene.meshCounts[id, default: 0]) people").font(LimmrTheme.font(12)).foregroundColor(LimmrTheme.ink3),at:CGPoint(x:box.midX,y:box.minY+28))
    }
  }
}
