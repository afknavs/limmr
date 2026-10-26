import AppKit
let directory = URL(fileURLWithPath: CommandLine.arguments[1])
let iconset = directory.appendingPathComponent("Limmr.iconset")
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
for points in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let size = points * scale
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: size * 4, bitsPerPixel: 32)!
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        let side = CGFloat(size)
        let tile = NSBezierPath(roundedRect: NSRect(x: 0, y: 0, width: side, height: side), xRadius: side * 0.225, yRadius: side * 0.225)
        // A paper tile with the faint top-lit falloff macOS icons have; not a decorative gradient.
        NSGradient(starting: NSColor(calibratedWhite: 0.975, alpha: 1), ending: NSColor(calibratedWhite: 0.955, alpha: 1))!.draw(in: tile, angle: -90)
        // Limmr's mark: one continuous thin ring, with dots at 10, 2 and 6 o'clock.
        let centre = NSPoint(x: side * 0.5, y: side * 0.5), radius = side * 0.265
        let at = { (degrees: CGFloat) in
            NSPoint(x: centre.x + radius * cos(degrees * .pi / 180), y: centre.y + radius * sin(degrees * .pi / 180)) }
        let ring = NSBezierPath(ovalIn: NSRect(x: centre.x - radius, y: centre.y - radius, width: radius * 2, height: radius * 2))
        let ink = NSColor(calibratedRed: 0.11, green: 0.11, blue: 0.13, alpha: 1)
        ink.setStroke()
        ring.lineWidth = max(1, side * 0.022)
        ring.stroke()
        for angle in [150.0, 30.0, 270.0] {
            let c = at(CGFloat(angle)), r = side * 0.047
            (angle == 30 ? NSColor(calibratedRed: 10.0 / 255, green: 132.0 / 255, blue: 1.0, alpha: 1) : ink).setFill()
            NSBezierPath(ovalIn: NSRect(x: c.x - r, y: c.y - r, width: 2 * r, height: 2 * r)).fill()
        }
        NSGraphicsContext.restoreGraphicsState()
        try bitmap.representation(using: .png, properties: [:])!.write(to: iconset.appendingPathComponent("icon_\(points)x\(points)\(scale == 2 ? "@2x" : "").png"))
    }
}
// PNG-backed ICNS chunks avoid iconutil's external image-conversion service.
var chunks = Data()
func bigEndian(_ value: Int) -> Data {
    var number = UInt32(value).bigEndian
    return withUnsafeBytes(of: &number) { Data($0) }
}
for (type, name) in [("icp4", "icon_16x16.png"), ("icp5", "icon_32x32.png"), ("icp6", "icon_32x32@2x.png"), ("ic07", "icon_128x128.png"), ("ic08", "icon_256x256.png"), ("ic09", "icon_512x512.png"), ("ic10", "icon_512x512@2x.png")] {
    let png = try Data(contentsOf: iconset.appendingPathComponent(name))
    chunks.append(Data(type.utf8)); chunks.append(bigEndian(png.count + 8)); chunks.append(png)
}
var icns = Data("icns".utf8)
icns.append(bigEndian(chunks.count + 8)); icns.append(chunks)
try icns.write(to: directory.appendingPathComponent("Limmr.icns"), options: .atomic)
