import CoreImage.CIFilterBuiltins
import SwiftUI
import LimmrKit

struct QRCode: View {
  let value: String
  var body: some View {
    Group {
      if let image = Self.image(value) {
        GeometryReader { _ in
          QRModules(pixels: Self.modules(image), count: image.width)
            .fill(.black)

        }
      } else {
        VStack {
          Image(systemName: "qrcode")
          Text("Unable to draw this code. Try linking with a phone number.").font(.caption)
            .multilineTextAlignment(.center)
        }.foregroundStyle(.black).padding(20)
      }
    }.padding(8).background(.white, in: RoundedRectangle(cornerRadius: 16)).environment(
      \.colorScheme, .light
    ).accessibilityLabel("WhatsApp linking QR code")
  }
  private static func modules(_ image: CGImage) -> [UInt8] {
    var pixels = [UInt8](repeating: 255, count: image.width * image.height * 4)
    pixels.withUnsafeMutableBytes { bytes in
      guard
        let context = CGContext(
          data: bytes.baseAddress, width: image.width, height: image.height,
          bitsPerComponent: 8, bytesPerRow: image.width * 4,
          space: CGColorSpaceCreateDeviceRGB(),
          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)
      else { return }
      context.interpolationQuality = .none
      context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
    }
    return pixels
  }

  static func image(_ string: String) -> CGImage? {
    // Synthetic snapshots/tests already have verified module images. Resolve
    // these before entering Core Image's process-global renderer initialization,
    // which can block a headless test executor during concurrent UI work.
    if string == "LIMMR-DEMO-PAIRING-NOT-A-REAL-SESSION" { return DemoQRCode.image }
    if string == DemoQRCode.longURL { return DemoQRCode.longImage }
    let filter = CIFilter.qrCodeGenerator()
    filter.message = Data(string.utf8)
    filter.correctionLevel = "M"
    if let output = filter.outputImage,
      let image = CIContext(options: [.useSoftwareRenderer: true]).createCGImage(
        output, from: output.extent)
    {
      return image
    }
    return nil
  }
}

/// Draw modules as integer-aligned vector squares, including in offscreen snapshots.
private struct QRModules: Shape {
  let pixels: [UInt8]
  let count: Int
  func path(in rect: CGRect) -> Path {
    let scale = max(1, floor(min(rect.width, rect.height) / CGFloat(count + 8)))
    let origin = CGPoint(
      x: floor((rect.width - CGFloat(count + 8) * scale) / 2),
      y: floor((rect.height - CGFloat(count + 8) * scale) / 2))
    var path = Path()
    for y in 0..<count {
      for x in 0..<count where pixels[(y * count + x) * 4] < 128 {
        path.addRect(
          CGRect(
            x: origin.x + CGFloat(x + 4) * scale, y: origin.y + CGFloat(y + 4) * scale,
            width: scale, height: scale))
      }
    }
    return path
  }
}
