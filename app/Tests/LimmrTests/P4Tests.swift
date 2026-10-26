import AppKit
import Testing
@testable import Limmr
@testable import LimmrKit

@Suite("P4 dense map") struct P4Tests {
 @Test func geometryInvariants() throws {
  let dense = P4DenseMapFixture.map
  #expect(dense.nodes.filter { $0.kind == "person" }.count == 300)
  #expect(P4DenseMapFixture.circles.count == 8)
  #expect(P4DenseMapFixture.circles.allSatisfy { $0.people.count == 50 })
  let map = UnderstandingPresentation.calmMap(dense)
  #expect(map.nodes.filter { $0.kind == "person" }.count == 120)
  for width in [524.0,760,1100] {
   var l = UnderstandingLayout();let size = CGSize(width:width,height:400)
   l.update(map:map,size:size)
   #expect(l.positions.count == map.nodes.count)
   let first=l.positions, labels=l.labels
   l.update(map:map,size:size)
   #expect(first == l.positions && labels == l.labels)
   for circle in map.nodes.filter({$0.kind == "circle"}) {
    let members=map.nodes.filter {$0.kind == "person" && UnderstandingLayout.memberships($0,map:map).contains(circle.id)}
    #expect(!members.isEmpty)
    #expect(members.filter {l.labels[$0.id] != nil}.count == 0)
    #expect(l.labels[circle.id] != nil)
    #expect(l.labelText[circle.id]?.replacingOccurrences(of:"\n",with:" ") == circle.title)
    let a=try #require(l.positions[circle.id])
    let d=members.filter {UnderstandingLayout.primary($0,map:map) == circle.id}.sorted(by:UnderstandingLayout.recent).compactMap {l.positions[$0.id]}.map {hypot($0.x-a.x,$0.y-a.y)}
    #expect(zip(d,d.dropFirst()).allSatisfy {$0 <= $1+0.01})
   }
   checkLabels(l)
   for circle in map.nodes.filter({ $0.kind == "circle" }) {
    if l.positions[circle.id] != nil {
      let badge = try #require(l.overviewBadge(circle.id))
      #expect(!l.labels.values.contains { $0.intersects(badge) })
    }
   }
   l.placeLabels(map:map,focused:false,extra:"p4-peer-0");checkLabels(l)
  }
  let focus=UnderstandingPresentation.calmMap(dense,focus:"p4-circle-0")
  var l=UnderstandingLayout();l.update(map:focus,size:CGSize(width:760,height:600),focused:true)
  #expect(focus.nodes.count == 51)
  #expect(l.labels.count == focus.nodes.count)
  checkLabels(l)
 }
 func checkLabels(_ l:UnderstandingLayout) {
  let boxes=Array(l.labels.values)
  for (i,b) in boxes.enumerated() {
   #expect(CGRect(origin:.zero,size:l.size).contains(b))
   for other in boxes.dropFirst(i+1) {#expect(!b.intersects(other))}
  }
 }
 @Test func footerAndTiers() {
  #expect(UnderstandingPresentation.wordEllipsis("A complete sentence with a long ending",limit:22) == "A complete sentence…")
  #expect(Set(P4DenseMapFixture.map.nodes.filter {$0.kind == "person"}.map(UnderstandingLayout.radius)).count == 3)
 }
 @Test func backoffAndCancellation() async throws {
  let b=P4RetryBackend(failures:4), waits=P4Waits()
  _ = try await MapLoadRetry.request(b,path:"/v1/circles",pause:{await waits.add($0)})
  #expect(await b.attempts == 5)
  #expect(await waits.values == MapLoadRetry.delays)
  let c=P4RetryBackend(failures:10)
  do { _ = try await MapLoadRetry.request(c,path:"/v1/map",pause:{_ in throw CancellationError()});Issue.record("Cancellation swallowed") } catch is CancellationError {}
  #expect(await c.attempts == 1)
 }
 @MainActor @Test func slowResponseAndLastGoodData() async {
  let b=P4RetryBackend(failures:0,slow:true)
  let store=AppStore(demo:true,snapshot:true,backend:b)
  let refresh = Task { await store.refresh() }
  await b.gate.waitUntilEntered()
  #expect(store.circles.isEmpty)
  await b.gate.release()
  await refresh.value
  #expect(!store.circles.isEmpty && !store.circleMap.nodes.isEmpty)
  let circles=store.circles
  await b.fail(10);await store.refresh()
  #expect(store.circles == circles)
  #expect(store.loadFailures["/v1/circles"] != nil)
 }
}
private actor P4Waits {
 var values:[Duration]=[]
 func add(_ v:Duration) {values.append(v)}
}
private actor P4RetryBackend:Backend {
 let gate = TestGate()
 var failures:Int;var attempts=0;let slow:Bool;let demo=DemoBackend()
 init(failures:Int,slow:Bool=false) {self.failures=failures;self.slow=slow}
 func fail(_ count:Int) {failures=count}
 func request(_ path:String,method:String,body:Data?) async throws -> Data {
  if path == "/v1/circles" || path == "/v1/map" {
   attempts += 1
   if failures > 0 {failures -= 1;throw BackendError.http(503)}
   if slow {await gate.enter()}
  }
  return try await demo.request(path,method:method,body:body)
 }
 nonisolated func events(_ path:String)->AsyncThrowingStream<SSEEvent,Error> {AsyncThrowingStream {$0.finish()}}
}
