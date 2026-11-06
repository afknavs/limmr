import AppKit
import ImageIO

/// Native source-list vibrancy and Liquid Glass live in WindowServer's compositor.
/// cacheDisplay omits them. Capture only this invented, unbundled snapshot window.
@MainActor enum N1NativeSnapshot {
  static func write(window: NSWindow, to path: String, size: NSSize) throws {
    let temporary = URL(fileURLWithPath: ProcessInfo.processInfo.environment["LIMMR_HOME"]!)
      .appendingPathComponent("native-window.png")
    let process = Process()
    process.executableURL = URL(fileURLWithPath: "/usr/sbin/screencapture")
    process.arguments = ["-x", "-o", "-l", String(window.windowNumber), temporary.path]
    process.standardInput = FileHandle.nullDevice
    try process.run(); process.waitUntilExit()
    guard process.terminationStatus == 0,
      let source = CGImageSourceCreateWithURL(temporary as CFURL, nil),
      let image = CGImageSourceCreateThumbnailAtIndex(source, 0, [
        kCGImageSourceCreateThumbnailFromImageAlways: true,
        kCGImageSourceThumbnailMaxPixelSize: Int(max(size.width, size.height)),
        kCGImageSourceCreateThumbnailWithTransform: true
      ] as CFDictionary) else { throw CocoaError(.fileReadUnknown) }
    // Normalize Retina captures to the requested one-pixel-per-point PNG contract.
    let bitmap = NSBitmapImageRep(cgImage: image)
    guard let png = bitmap.representation(using: .png, properties: [:]) else { throw CocoaError(.fileWriteUnknown) }
    try png.write(to: URL(fileURLWithPath: path), options: .atomic)
  }
}
