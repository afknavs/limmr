import AppKit
import Vision

let expected =
  ProcessInfo.processInfo.environment["LIMMR_QR_FIXTURE"] == "long"
  ? "https://limmr.example/pair?code=" + String(repeating: "abcdEF12", count: 30)
  : "LIMMR-DEMO-PAIRING-NOT-A-REAL-SESSION"
for path in CommandLine.arguments.dropFirst() {
  let request = VNDetectBarcodesRequest()
  request.symbologies = [.qr]
  request.revision = VNDetectBarcodesRequestRevision1
  request.usesCPUOnly = true
  let handler = VNImageRequestHandler(url: URL(fileURLWithPath: path))
  do { try handler.perform([request]) } catch {
    print("Demo QR verification unavailable in this environment.")
    exit(2)
  }
  let decoded = request.results?.contains { $0.payloadStringValue == expected } == true
  print("Demo QR decoded: \(decoded)")
  if !decoded { exit(1) }
}
