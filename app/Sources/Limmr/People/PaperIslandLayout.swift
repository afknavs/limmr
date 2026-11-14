import Foundation
import LimmrKit

/// Immutable, deterministic geometry. Runs on NetworkMapLayoutCache's detached task.
enum PaperIslandLayout {
  struct Result {
    var positions: [String: CGPoint]
    var diameters: [String: Double]
    var hubs: [String: CGPoint]
    var labels: [String: CGPoint]
    var labelFrames: [String: CGRect]
    var counts: [String: Int]
    var memberships: [String: [String]]
    var faces: Set<String>
    var edges: [CircleMapEdge]
    var net: [CircleMapEdge]
    var size: CGSize
  }
  struct Particle { var id: String; var x: Double; var y: Double; var radius: Double; var face: Bool }
  struct Cell: Hashable { let x: Int; let y: Int }
  static func relax(_ points: inout [Particle], passes: Int = 260, spacingScale: Double = 1) {
    guard points.count > 1 else { return }
    points.withUnsafeMutableBufferPointer { buffer in
      for _ in 0..<passes {
        var buckets: [Cell: [Int]] = [:]
        for i in buffer.indices { buckets[Cell(x:Int(floor(buffer[i].x/64)),y:Int(floor(buffer[i].y/64))),default:[]].append(i) }
        var moved = false
        for i in buffer.indices {
          let cell = Cell(x:Int(floor(buffer[i].x/64)),y:Int(floor(buffer[i].y/64)))
          for cx in -1...1 { for cy in -1...1 { for j in buckets[Cell(x:cell.x+cx,y:cell.y+cy)] ?? [] where j > i {
            let dx = buffer[j].x-buffer[i].x, dy = buffer[j].y-buffer[i].y
            let minimum = buffer[i].radius+buffer[j].radius+(buffer[i].face && buffer[j].face ? 6 : buffer[i].face || buffer[j].face ? 5 : 9.5)*spacingScale
            let square = dx*dx+dy*dy
            guard square < minimum*minimum-0.00001 else { continue }
            let distance = max(0.001, sqrt(square)), f = (minimum-distance)/distance
            let wa = buffer[i].face ? 0.25 : 1.0, wb = buffer[j].face ? 0.25 : 1.0
            let vx = square < 0.000001 ? minimum : dx*f, vy = square < 0.000001 ? 0 : dy*f
            buffer[i].x -= vx*wa/(wa+wb); buffer[i].y -= vy*wa/(wa+wb)
            buffer[j].x += vx*wb/(wa+wb); buffer[j].y += vy*wb/(wa+wb); moved = true
          } } }
        }
        if !moved { break }
      }
    }
  }
  static func prepare(_ map: CircleMap, size: CGSize = CGSize(width: 712, height: 430)) -> Result {
    let people = map.nodes.filter { $0.kind == "person" && $0.id != "me" }.sorted { $0.id < $1.id }
    let circles = map.nodes.filter { $0.kind == "circle" }.sorted { $0.id < $1.id }
    let valid = Set(circles.map(\.id))
    let memberships = Dictionary(uniqueKeysWithValues: people.map { ($0.id, UnderstandingLayout.memberships($0, map: map).filter { valid.contains($0) }.sorted()) })
    var ranked: [String: [CircleMapNode]] = [:], ranks: [String: Int] = [:], counts: [String: Int] = [:]
    for circle in circles {
      let list = people.filter { memberships[$0.id, default: []].contains(circle.id) }.sorted {
        let a = $0.closeness ?? $0.weight ?? 0, b = $1.closeness ?? $1.weight ?? 0
        return a == b ? $0.id < $1.id : a > b
      }
      ranked[circle.id] = list; counts[circle.id] = list.count
      for (rank,p) in list.enumerated() { ranks[p.id] = min(ranks[p.id] ?? rank, rank) }
    }
    var diameters: [String: Double] = ["me": 40]
    for person in people {
      let rank = ranks[person.id] ?? 9999
      let small = memberships[person.id, default: []].contains { counts[$0, default: 0] <= 20 }
      let jitter = Double(MapSceneMotion.stableHash(person.id) % 1000)/1000
      diameters[person.id] = person.needsYou == true ? max(26, rank == 0 ? 34 : 26) : rank == 0 ? 34 : rank < 6 || small ? 26 : 4 + 4.8*jitter*jitter
    }
    let faces = Set(diameters.filter { $0.value >= 26 }.map(\.key))
    let order = circles.sorted { counts[$0.id] == counts[$1.id] ? $0.id < $1.id : counts[$0.id, default: 0] > counts[$1.id, default: 0] }
    var locals: [String: [Particle]] = [:], radii: [String: Double] = [:]
    for circle in order {
      let d = counts[circle.id, default: 0] > 20 ? 18.0 : 44.0
      var particles = ranked[circle.id, default: []].enumerated().compactMap { i,p -> Particle? in
        guard memberships[p.id, default: []].count == 1 else { return nil }
        let hash = MapSceneMotion.stableHash(p.id), jitter = Double(hash % 10000)/10000
        let angle = Double(i)*2.39996+jitter*0.6, radius = d*0.56*sqrt(Double(i)+0.35)+(jitter-0.5)*d*0.45
        let diameter = diameters[p.id, default: 5]
        return Particle(id: p.id, x: cos(angle)*radius, y: sin(angle)*radius, radius: diameter/2, face: diameter >= 26)
      }
      relax(&particles)
      locals[circle.id] = particles
      radii[circle.id] = max(28, particles.map { hypot($0.x,$0.y)+$0.radius }.max() ?? 28)+16
    }
    let you = CGPoint.zero
    var hubs: [String: CGPoint] = [:]
    let angles = [-0.3,0.85,2.4,-2.4]
    let aspect = sqrt((size.width/size.height)/1.65)
    for (index,circle) in order.enumerated() {
      let radius = radii[circle.id, default: 30]
      let angle = index < 4 ? angles[index] : Double(index)*2.39996
      var best: CGPoint?
      for ring in 0..<180 {
        let distance = radius+64+Double(ring)*4
        for step in 0..<24 {
          let a = angle+Double(step/2)*(step % 2 == 0 ? 0.12 : -0.12)
          let candidate = CGPoint(x: cos(a)*distance*1.45*aspect,y: sin(a)*distance*0.7/aspect)
          if hubs.allSatisfy({ id,p in hypot(candidate.x-p.x,candidate.y-p.y) >= radius+radii[id, default: 30]+48 }) { best = candidate; break }
        }
        if best != nil { break }
      }
      hubs[circle.id] = best ?? CGPoint(x: Double(index)*300,y: 300)
    }
    var particles = [Particle(id: "me",x: you.x,y: you.y,radius: 24,face: true)]
    for circle in order { let hub = hubs[circle.id]!
      for p in locals[circle.id, default: []] { particles.append(Particle(id:p.id,x:p.x+hub.x,y:p.y+hub.y,radius:p.radius,face:p.face)) }
    }
    for person in people where memberships[person.id, default: []].count > 1 {
      let centres = memberships[person.id, default: []].compactMap { hubs[$0] }
      particles.append(Particle(id: person.id,x: centres.map(\.x).reduce(0,+)/Double(centres.count),y: centres.map(\.y).reduce(0,+)/Double(centres.count),radius: diameters[person.id, default: 5]/2,face: diameters[person.id, default: 5] >= 26))
    }
    // Local islands are already relaxed. Shared nodes participate in a final global pass.
    relax(&particles)
    let fitted = fit(particles, hubs: hubs, circles: order, memberships: memberships, size: size,
                     small: people.count <= 12 && circles.count <= 1, protectedFaces:Set(people.filter { $0.needsYou == true }.map(\.id)))
    var positions = fitted.positions
    diameters = fitted.diameters; hubs = fitted.hubs
    let labels = fitted.frames.mapValues { CGPoint(x:$0.midX,y:$0.midY) }
    let scale = fitted.scale
    // Loose people stay around the perimeter, outside islands and the final plates.
    var loose: [CGPoint] = []
    let loosePeople = people.filter { memberships[$0.id, default: []].isEmpty }
    let occupied = positions.keys.sorted().compactMap { id -> (CGPoint,Double)? in
      guard id != "me", let p = positions[id] else { return nil }
      return (p,diameters[id,default:5]/2)
    }
    var nextLoose = 0
    for attempt in 0..<6000 {
      guard nextLoose < loosePeople.count else { break }
      let person = loosePeople[nextLoose], seed = Double(MapSceneMotion.stableHash(person.id)%100000)/100000
      let x = 24+((seed+Double(attempt)*0.61803398875).truncatingRemainder(dividingBy:1))*(size.width-48)
      let y = 24+((seed*0.7+Double(attempt)*0.41421356237).truncatingRemainder(dividingBy:1))*(size.height-48)
      let p = CGPoint(x:x,y:y)
      guard hypot(x-positions["me"]!.x,y-positions["me"]!.y) >= 56,
        loose.allSatisfy({ hypot(x-$0.x,y-$0.y) >= 58 }),
        fitted.frames.values.allSatisfy({ !intersects($0, point:p, radius:19) }),
        occupied.allSatisfy({ hypot(x-$0.0.x,y-$0.0.y) >= $0.1+44 }) else { continue }
      positions[person.id] = p; loose.append(p); diameters[person.id] = 4.4; nextLoose += 1
    }
    var groups: [String: Set<String>] = [:]
    for person in people where !memberships[person.id, default: []].isEmpty {
      for group in person.sharedGroupIds ?? person.groupsInCommon?.map(\.id) ?? [] { groups[group, default: []].insert(person.id) }
    }
    var realPairs: Set<String> = [], edges: [CircleMapEdge] = []
    for group in groups.keys.sorted() { let ids = groups[group]!.sorted()
      for i in ids.indices { for j in (i+1)..<ids.count { let key = ids[i]+":"+ids[j]
        if realPairs.insert(key).inserted { edges.append(.init(source:ids[i],target:ids[j],kind:"shared_group",state:"confirmed")) }
      } }
    }
    var netKeys: Set<String> = [], net: [CircleMapEdge] = []
    for circle in order {
      let ids = ranked[circle.id, default: []].map(\.id).filter { positions[$0] != nil }.sorted()
      for pair in triangulate(ids.compactMap { positions[$0] }) {
        let a = ids[pair.0], b = ids[pair.1], key = min(a,b)+":"+max(a,b)
        let p = positions[a]!,q = positions[b]!, limit = counts[circle.id, default: 0] > 20 ? 33.0 : 90.0
        if realPairs.contains(key), hypot(p.x-q.x,p.y-q.y) < limit*scale, netKeys.insert(key).inserted { net.append(.init(source:a,target:b,kind:"shared_group",state:"confirmed")) }
      }
    }
    return Result(positions:positions,diameters:diameters,hubs:hubs,labels:labels,labelFrames:fitted.frames,counts:counts,memberships:memberships,faces:faces,edges:edges,net:net,size:size)
  }
  struct Fit {
    var positions: [String:CGPoint]
    var diameters: [String:Double]
    var hubs: [String:CGPoint]
    var frames: [String:CGRect]
    var scale: Double
  }
  static func intersects(_ box: CGRect, point: CGPoint, radius: Double) -> Bool {
    let dx = max(box.minX-point.x, 0, point.x-box.maxX)
    let dy = max(box.minY-point.y, 0, point.y-box.maxY)
    return dx*dx+dy*dy < radius*radius
  }
  static func labelSize(_ title: String) -> CGSize {
    CGSize(width:min(164,max(88,Double(title.count)*7.5+16)),height:40)
  }
  /// Fit the actual bodies plus fixed-size plates, never a guessed circle radius.
  /// The finite scale search leaves enough negative space for every plate. All tests
  /// use these exact rectangles, which are also used by rendering and hit testing.
  static func fit(_ particles: [Particle], hubs: [String:CGPoint], circles: [CircleMapNode],
                  memberships: [String:[String]], size: CGSize, small: Bool, protectedFaces: Set<String>) -> Fit {
    let bounds = CGRect(origin:.zero,size:size).insetBy(dx:24,dy:24)
    var raw = CGRect.null
    for p in particles { raw = raw.union(CGRect(x:p.x-p.radius,y:p.y-p.radius,width:p.radius*2,height:p.radius*2)) }
    let maximum = min(small ? 1.6 : Double.infinity,
                      (bounds.width-26)/max(1,raw.width), (bounds.height-26-48)/max(1,raw.height))
    var result = Fit(positions:[:],diameters:[:],hubs:[:],frames:[:],scale:maximum)
    for step in 0..<24 {
      let scale = maximum*pow(0.94,Double(step))
      func transform(_ p: CGPoint) -> CGPoint {
        CGPoint(x:(p.x-raw.midX)*scale+size.width/2,y:(p.y-raw.midY)*scale+(size.height-48)/2)
      }
      let diameters = Dictionary(uniqueKeysWithValues:particles.map { p in
        let base = p.id == "me" ? 40 : p.radius*2
        return (p.id, p.id == "me" ? 40 : protectedFaces.contains(p.id) ? max(26,base*scale) : base*scale)
      })
      var fitted = particles.map { p -> Particle in
        let q = transform(CGPoint(x:p.x,y:p.y))
        return Particle(id:p.id,x:q.x,y:q.y,radius:diameters[p.id]!/2,face:p.face)
      }
      relax(&fitted, spacingScale:scale)
      let points = Dictionary(uniqueKeysWithValues:fitted.map { ($0.id,CGPoint(x:$0.x,y:$0.y)) })
      let obstacles = fitted.map { (points[$0.id]!,max(19,$0.radius+13)) }
      let owner = points["me"]!
      let you = CGRect(x:owner.x-18,y:owner.y+diameters["me"]!/2+3,width:36,height:20)
      var frames: [String:CGRect] = [:]
      for circle in circles {
        let members = particles.filter { memberships[$0.id,default:[]].contains(circle.id) }
        var shape = CGRect.null
        for p in members {
          let q = points[p.id]!, r = diameters[p.id]!/2+13
          shape = shape.union(CGRect(x:q.x-r,y:q.y-r,width:r*2,height:r*2))
        }
        let hub = transform(hubs[circle.id]!), plate = labelSize(circle.title)
        if shape.isNull { shape = CGRect(x:hub.x-12,y:hub.y-12,width:24,height:24) }
        func valid(_ box: CGRect) -> Bool {
          bounds.contains(box) && !box.intersects(you) &&
            frames.values.allSatisfy { !$0.insetBy(dx:-4,dy:-4).intersects(box) } &&
            obstacles.allSatisfy { !intersects(box,point:$0.0,radius:$0.1+3) }
        }
        var candidates: [CGRect] = []
        for shift in [0.0,-8,8,-16,16,-32,32,-48,48,-64,64] {
          candidates.append(CGRect(x:hub.x-plate.width/2+shift,y:shape.maxY+6,width:plate.width,height:plate.height))
        }
        candidates.append(CGRect(x:hub.x-plate.width/2,y:shape.minY-plate.height-6,width:plate.width,height:plate.height))
        candidates.append(CGRect(x:shape.maxX+6,y:hub.y-20,width:plate.width,height:plate.height))
        candidates.append(CGRect(x:shape.minX-plate.width-6,y:hub.y-20,width:plate.width,height:plate.height))
        if let box = candidates.first(where:valid) { frames[circle.id] = box; continue }
        // Search all free negative space, preferring positions near the lower rim.
        var free: [(Double,CGRect)] = []
        for y in stride(from:bounds.minY,through:bounds.maxY-plate.height,by:8) {
          for x in stride(from:bounds.minX,through:bounds.maxX-plate.width,by:8) {
            let box = CGRect(x:x,y:y,width:plate.width,height:plate.height)
            guard valid(box) else { continue }
            let score = hypot(box.midX-hub.x,box.minY-shape.maxY) + (box.midY < hub.y ? 40 : 0)
            free.append((score,box))
          }
        }
        if let box = free.min(by: { $0.0 < $1.0 })?.1 { frames[circle.id] = box }
      }
      result = Fit(positions:points,diameters:diameters,hubs:hubs.mapValues(transform),frames:frames,scale:scale)
      let inside = fitted.allSatisfy { p in
        let r = p.radius+13
        return bounds.contains(CGRect(x:p.x-r,y:p.y-r,width:r*2,height:r*2))
      }
      if inside && frames.count == circles.count { break }
    }
    // Expand the completed arrangement to the 24-point inset. Plate text and
    // You stay readable at a fixed size; increasing separation preserves clearance.
    func extent(_ factor: Double) -> CGRect {
      var box = CGRect.null
      for p in particles {
        let q = result.positions[p.id]!, diameter = result.diameters[p.id]!
        let r = (p.id == "me" ? diameter : diameter*factor)/2+13
        box = box.union(CGRect(x:q.x*factor-r,y:q.y*factor-r,width:r*2,height:r*2))
      }
      let owner = result.positions["me"]!
      box = box.union(CGRect(x:owner.x*factor-18,y:owner.y*factor+23,width:36,height:20))
      for plate in result.frames.values {
        box = box.union(CGRect(x:plate.midX*factor-plate.width/2,y:plate.midY*factor-plate.height/2,width:plate.width,height:plate.height))
      }
      return box
    }
    var low = 1.0, high = small ? max(1,1.6/result.scale) : 64.0
    for _ in 0..<28 {
      let factor = (low+high)/2, box = extent(factor)
      if box.width <= bounds.width && box.height <= bounds.height { low = factor } else { high = factor }
    }
    let box = extent(low), dx = size.width/2-box.midX, dy = size.height/2-box.midY
    result.positions = result.positions.mapValues { CGPoint(x:$0.x*low+dx,y:$0.y*low+dy) }
    result.hubs = result.hubs.mapValues { CGPoint(x:$0.x*low+dx,y:$0.y*low+dy) }
    result.diameters = result.diameters.mapValues { $0*low }
    result.diameters["me"] = 40
    result.frames = result.frames.mapValues { CGRect(x:$0.midX*low+dx-$0.width/2,y:$0.midY*low+dy-$0.height/2,width:$0.width,height:$0.height) }
    result.scale *= low
    return result
  }
  /// Bowyer-Watson, with stable input ordering and undirected edge deduplication.
  static func triangulate(_ input: [CGPoint]) -> [(Int,Int)] {
    guard input.count > 2 else { return input.count == 2 ? [(0,1)] : [] }
    let n = input.count, d = max(1,(input.map(\.x).max()! - input.map(\.x).min()!), (input.map(\.y).max()! - input.map(\.y).min()!))*20
    let mx = (input.map(\.x).min()!+input.map(\.x).max()!)/2, my = (input.map(\.y).min()!+input.map(\.y).max()!)/2
    let p = input+[CGPoint(x:mx-d,y:my-d),CGPoint(x:mx+d,y:my-d),CGPoint(x:mx,y:my+d)]
    struct Triangle { let a:Int; let b:Int; let c:Int; let x:Double; let y:Double; let r:Double }
    struct Edge: Hashable { let a:Int; let b:Int; init(_ a:Int,_ b:Int) { self.a = min(a,b); self.b = max(a,b) } }
    func triangle(_ a:Int,_ b:Int,_ c:Int) -> Triangle? {
      let A=p[a],B=p[b],C=p[c], q=2*(A.x*(B.y-C.y)+B.x*(C.y-A.y)+C.x*(A.y-B.y))
      guard abs(q)>0.0000001 else { return nil }
      let aa=A.x*A.x+A.y*A.y,bb=B.x*B.x+B.y*B.y,cc=C.x*C.x+C.y*C.y
      let x=(aa*(B.y-C.y)+bb*(C.y-A.y)+cc*(A.y-B.y))/q, y=(aa*(C.x-B.x)+bb*(A.x-C.x)+cc*(B.x-A.x))/q
      return Triangle(a:a,b:b,c:c,x:x,y:y,r:(A.x-x)*(A.x-x)+(A.y-y)*(A.y-y))
    }
    var triangles = [triangle(n,n+1,n+2)!]
    for i in 0..<n {
      var keep:[Triangle]=[], boundary:[Edge:Int]=[:]
      for t in triangles {
        if (p[i].x-t.x)*(p[i].x-t.x)+(p[i].y-t.y)*(p[i].y-t.y)<t.r {
          for edge in [Edge(t.a,t.b),Edge(t.b,t.c),Edge(t.c,t.a)] { boundary[edge,default:0] += 1 }
        } else { keep.append(t) }
      }
      for edge in boundary.keys.sorted(by: { $0.a == $1.a ? $0.b < $1.b : $0.a < $1.a }) where boundary[edge] == 1 {
        if let t=triangle(edge.a,edge.b,i) { keep.append(t) }
      }
      triangles=keep
    }
    var output:Set<Edge>=[]
    for t in triangles where t.a<n && t.b<n && t.c<n { output.formUnion([Edge(t.a,t.b),Edge(t.b,t.c),Edge(t.c,t.a)]) }
    return output.sorted { $0.a == $1.a ? $0.b < $1.b : $0.a < $1.a }.map { ($0.a,$0.b) }
  }
}
