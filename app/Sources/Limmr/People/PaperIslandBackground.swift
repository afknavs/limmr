import AppKit
import SwiftUI
import LimmrKit

/// One raster per immutable layout and appearance; pan/zoom only transforms it.
@MainActor enum PaperIslandBackground {
  final class Box { let image: Image; init(_ image: Image) { self.image = image } }
  static let cache: NSCache<NSString, Box> = { let value = NSCache<NSString, Box>(); value.countLimit = 8; return value }()
  static func image(scene: NetworkMapProjection, dark: Bool) -> Image? {
    let key = ((dark ? "dark:" : "light:")+scene.positions.keys.sorted().map { id in
      let p = scene.positions[id]!
      return "\(id):\(p.x):\(p.y):\(scene.meshDiameters[id, default: 0])"
    }.joined(separator: ";")) as NSString
    if let hit = cache.object(forKey: key) { return hit.image }
    let view = Canvas { context, _ in
      context.addFilter(.alphaThreshold(min: 0.45, color: LimmrTheme.surface))
      context.addFilter(.blur(radius: 9))
      context.drawLayer { layer in
        for (id,memberships) in scene.meshMemberships where !memberships.isEmpty {
          guard let p = scene.positions[id] else { continue }
          let r = scene.meshDiameters[id, default: 5]/2+13
          layer.fill(Path(ellipseIn: CGRect(x:p.x-r,y:p.y-r,width:r*2,height:r*2)),with:.color(.white))
        }
      }
    }.frame(width:scene.world.width,height:scene.world.height).environment(\.colorScheme,dark ? .dark : .light)
    let renderer = ImageRenderer(content:view); renderer.scale = 2
    guard let cg = renderer.cgImage else { return nil }
    let image = Image(decorative:cg,scale:2); cache.setObject(Box(image),forKey:key)
    return image
  }
}
