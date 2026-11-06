import AppKit

// "Conductor": a bold black L for Limmr with one orange square, on a warm paper tile.
// Drawn on the macOS icon grid: an 824/1024 continuous-corner body centred on the canvas, soft drop shadow.
func superellipse(_ r: NSRect) -> NSBezierPath {
    let p = NSBezierPath(), n: CGFloat = 5, steps = 720
    for i in 0...steps {
        let t = CGFloat(i) / CGFloat(steps) * 2 * .pi, c = cos(t), s = sin(t)
        let point = NSPoint(x: r.midX + r.width / 2 * (c < 0 ? -1 : 1) * pow(abs(c), 2 / n),
                            y: r.midY + r.height / 2 * (s < 0 ? -1 : 1) * pow(abs(s), 2 / n))
        i == 0 ? p.move(to: point) : p.line(to: point)
    }
    p.close()
    return p
}
func rgb(_ hex: UInt32) -> NSColor {
    NSColor(srgbRed: CGFloat(hex >> 16 & 0xff) / 255, green: CGFloat(hex >> 8 & 0xff) / 255, blue: CGFloat(hex & 0xff) / 255, alpha: 1)
}
func drawConductor(side: CGFloat) {
    let inset = side * 100 / 1024, body = NSRect(x: inset, y: inset, width: side - 2 * inset, height: side - 2 * inset)
    let tile = superellipse(body)
    NSGraphicsContext.saveGraphicsState()
    let shadow = NSShadow()
    shadow.shadowColor = NSColor(white: 0, alpha: 0.22); shadow.shadowBlurRadius = side * 0.02; shadow.shadowOffset = NSSize(width: 0, height: -side * 0.009)
    shadow.set(); rgb(0xF4EFE7).setFill(); tile.fill()
    NSGraphicsContext.restoreGraphicsState()
    NSGradient(starting: rgb(0xFCFAF6), ending: rgb(0xF3EEE5))!.draw(in: tile, angle: -90)
    let b = body.width
    func box(_ x0: CGFloat, _ y0: CGFloat, _ x1: CGFloat, _ y1: CGFloat) -> NSRect {
        NSRect(x: body.minX + x0 * b, y: body.minY + y0 * b, width: (x1 - x0) * b, height: (y1 - y0) * b)
    }
    let r = b * 0.036, fillet = b * 0.03, ink = rgb(0x141413)
    ink.setFill()
    for part in [box(0.229, 0.250, 0.411, 0.878), box(0.178, 0.122, 0.822, 0.300), box(0.633, 0.122, 0.822, 0.533)] {
        NSBezierPath(roundedRect: part, xRadius: r, yRadius: r).fill()
    }
    // Concave fillets where the uprights meet the base, so the L reads as one shape.
    for (x, left) in [(0.411, true), (0.633, false), (0.229, false)] as [(CGFloat, Bool)] {
        let cx = body.minX + x * b, cy = body.minY + 0.300 * b
        let corner = NSBezierPath(), d: CGFloat = left ? 1 : -1
        corner.move(to: NSPoint(x: cx, y: cy))
        corner.line(to: NSPoint(x: cx + d * fillet, y: cy))
        corner.appendArc(withCenter: NSPoint(x: cx + d * fillet, y: cy + fillet), radius: fillet,
                         startAngle: 270, endAngle: left ? 180 : 360, clockwise: left)
        corner.close()
        corner.fill()
    }
    rgb(0xF4461F).setFill()
    NSBezierPath(roundedRect: box(0.496, 0.589, 0.733, 0.822), xRadius: b * 0.02, yRadius: b * 0.02).fill()
}
let directory = URL(fileURLWithPath: CommandLine.arguments[1])
let iconset = directory.appendingPathComponent("Limmr.iconset")
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
for points in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let size = points * scale
        let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: size, pixelsHigh: size, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: size * 4, bitsPerPixel: 32)!
        NSGraphicsContext.saveGraphicsState()
        NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
        drawConductor(side: CGFloat(size))
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
