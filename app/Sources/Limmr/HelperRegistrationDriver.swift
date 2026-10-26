import Darwin
import Foundation
import LimmrKit
import Security
import ServiceManagement

// CS_OPS_CDHASH reads the kernel's identity of the running image, even if its on-disk
// pathname has been replaced. SecCodeCopySigningInformation alone reads the disk signature.
@_silgen_name("csops")
private func helperCSOps(_ pid: Int32, _ operation: UInt32, _ buffer: UnsafeMutableRawPointer, _ size: Int) -> Int32

enum HelperCodeSignature {
  static func identity(_ executable: URL) throws -> HelperIdentity {
    var code: SecStaticCode?
    guard SecStaticCodeCreateWithPath(executable as CFURL, [], &code) == errSecSuccess, let code,
          SecStaticCodeCheckValidity(code, SecCSFlags(rawValue: kSecCSCheckAllArchitectures), nil) == errSecSuccess
    else { throw HelperUpdateFailure(reason: "The engine helper is missing or its signature is invalid. Reinstall Limmr and try again.") }
    var info: CFDictionary?
    guard SecCodeCopySigningInformation(code, [], &info) == errSecSuccess,
          let hash = (info as? [String: Any])?[kSecCodeInfoUnique as String] as? Data
    else { throw HelperUpdateFailure(reason: "The helper code fingerprint could not be read from its signature.") }
    var requirement: SecRequirement?
    let requirementText = "anchor apple generic and certificate leaf[field.1.2.840.113635.100.6.1.13] exists"
    guard SecRequirementCreateWithString(requirementText as CFString, [], &requirement) == errSecSuccess else { throw BackendError.invalidResponse }
    let developerID = SecStaticCodeCheckValidity(code, [], requirement) == errSecSuccess
    let receipt = EngineBuildReceipt(helper: executable, receipt: executable)
    var identity = HelperIdentity(sha256: try receipt.fingerprint(), codeHash: hash.hex, developerID: developerID)
    // A universal executable can run a different slice from Security's default selection
    // (for example arm64e.x1). Collect identities for each signed slice, not only the default.
    for offset in try universalOffsets(executable) {
      var slice: SecStaticCode?
      let attributes = [kSecCodeAttributeUniversalFileOffset as String: NSNumber(value: offset)] as CFDictionary
      guard SecStaticCodeCreateWithPathAndAttributes(executable as CFURL, [], attributes, &slice) == errSecSuccess,
            let slice else { throw BackendError.invalidResponse }
      var sliceInfo: CFDictionary?
      guard SecCodeCopySigningInformation(slice, [], &sliceInfo) == errSecSuccess,
            let sliceHash = (sliceInfo as? [String: Any])?[kSecCodeInfoUnique as String] as? Data
      else { throw BackendError.invalidResponse }
      identity.alternateCodeHashes.insert(sliceHash.hex)
    }
    return identity
  }

  private static func universalOffsets(_ executable: URL) throws -> [UInt64] {
    let file = try FileHandle(forReadingFrom: executable)
    defer { try? file.close() }
    guard let header = try file.read(upToCount: 8), header.count == 8 else { throw BackendError.invalidResponse }
    func number(_ data: Data, _ start: Int, _ size: Int, little: Bool) -> UInt64 {
      let bytes = Array(data[start..<(start + size)])
      return (little ? Array(bytes.reversed()) : bytes).reduce(0) { ($0 << 8) | UInt64($1) }
    }
    let magic = number(header, 0, 4, little: false)
    guard [0xcafebabe, 0xcafebabf, 0xbebafeca, 0xbfbafeca].contains(magic) else { return [] }
    let little = magic == 0xbebafeca || magic == 0xbfbafeca
    let wide = magic == 0xcafebabf || magic == 0xbfbafeca
    let count = Int(number(header, 4, 4, little: little)), stride = wide ? 32 : 20
    guard count > 0, count <= 64, let entries = try file.read(upToCount: count * stride), entries.count == count * stride
    else { throw BackendError.invalidResponse }
    return (0..<count).map { number(entries, $0 * stride + 8, wide ? 8 : 4, little: little) }
  }

  static func runningHash(_ pid: Int32) -> String? {
    var hash = [UInt8](repeating: 0, count: 20) // CS_CDHASH_LEN; SHA-256 cdhash is truncated by the kernel
    let result = hash.withUnsafeMutableBytes { helperCSOps(pid, 5, $0.baseAddress!, $0.count) }
    return result == 0 ? Data(hash).hex : nil
  }
}

@MainActor final class LaunchdHelperDriver: HelperRegistrationDriver {
  unowned let system: SystemActions
  private let helper: URL
  private let home: URL
  private let fallback: URL
  init(system: SystemActions, helper: URL? = nil, home: URL? = nil, fallback: URL? = nil) {
    self.system = system
    self.helper = helper ?? Bundle.main.bundleURL.appending(path: "Contents/Helpers/limmr-engine")
    self.home = home ?? system.paths.home
    self.fallback = fallback ?? FileManager.default.homeDirectoryForCurrentUser.appending(path: "Library/LaunchAgents/dev.limmr.engine.plist")
  }
  private var service: SMAppService { .agent(plistName: "dev.limmr.engine.plist") }
  private var domain: String { "gui/\(getuid())" }
  private var jobName: String { domain + "/dev.limmr.engine" }
  var registration: EngineRegistration {
    switch service.status {
    case .requiresApproval: return .needsApproval
    case .enabled: return .registered
    default:
      return FileManager.default.fileExists(atPath: fallback.path)
        || FileManager.default.fileExists(atPath: home.appending(path: "control-token").path) ? .registered : .absent
    }
  }
  func healthy() async -> Bool { await system.probe() }
  func readiness() async -> HelperReadiness {
    let health = await system.healthReadiness()
    if health != .unavailable { return health }
    guard let job = try? await job(), job.pid != nil else { return .unavailable }
    // A long-running helper that misses one probe gets a few seconds, not an instant failure.
    return .starting(progress: nil, grace: max(5, 30 - (job.age ?? 0)))
  }
  func identity() async throws -> HelperIdentity {
    let helper = helper
    return try await Task.detached(priority: .utility) { try HelperCodeSignature.identity(helper) }.value
  }
  func job() async throws -> HelperJob? {
    guard let output = try? await system.run("/bin/launchctl", ["print", jobName]) else { return nil }
    let pid = output.split(separator: "\n").compactMap { line -> Int32? in
      let line = line.trimmingCharacters(in: .whitespaces)
      guard line.hasPrefix("pid = ") else { return nil }
      return Int32(line.dropFirst(6))
    }.first
    let route: HelperRoute = output.contains("has LWCR") || output.contains("program identifier =")
      || output.contains("com.apple.xpc.ServiceManagement") ? .serviceManagement : .userAgent
    var executable: URL?
    if let pid {
      var buffer = [CChar](repeating: 0, count: 4 * Int(MAXPATHLEN))
      if proc_pidpath(pid, &buffer, UInt32(buffer.count)) > 0 {
        executable = URL(fileURLWithPath: String(decoding: buffer.prefix { $0 != 0 }.map { UInt8(bitPattern: $0) }, as: UTF8.self))
      }
    }
    var age: TimeInterval?
    if let pid {
      var info = proc_bsdinfo()
      let size = Int32(MemoryLayout<proc_bsdinfo>.size)
      if proc_pidinfo(pid, PROC_PIDTBSDINFO, 0, &info, size) == size {
        let start = TimeInterval(info.pbi_start_tvsec) + TimeInterval(info.pbi_start_tvusec) / 1_000_000
        age = max(0, Date().timeIntervalSince1970 - start)
      }
    }
    return HelperJob(route: route, pid: pid, codeHash: pid.flatMap(HelperCodeSignature.runningHash), executable: executable, age: age)
  }

  func retain(_ identity: HelperIdentity) async throws { _ = try await stage(helper, identity: identity) }

  func prepare(_ identity: HelperIdentity, route: HelperRoute) async throws -> HelperLaunchPlan {
    let staged = try await stage(helper, identity: identity)
    // Validate that launchd can execute this architecture without touching the real home, stores or ports.
    try await Self.preflight(staged)
    return HelperLaunchPlan(identity: identity, route: route, plist: try plist(executable: staged))
  }

  func backup(_ job: HelperJob) async throws -> HelperLaunchPlan {
    guard let runningHash = job.codeHash else { throw HelperUpdateFailure(reason: "The previous helper has no readable running fingerprint.") }
    // A previous launch/adoption retains the immutable image before a later install replaces
    // the bundle pathname. Legacy jobs may still have a usable executable at their original path.
    let retained = (try? FileManager.default.contentsOfDirectory(
      at: home.appending(path: "helpers"), includingPropertiesForKeys: nil,
      options: .skipsHiddenFiles)) ?? []
    let sources = (job.executable.map { [$0] } ?? []) + retained.map { $0.appending(path: "limmr-engine") }
    guard let source = sources.first(where: { (try? HelperCodeSignature.identity($0).matches(runningHash)) == true }) else {
      throw HelperUpdateFailure(reason: "No retained previous helper matches the running engine. Keep it running until a safe recovery copy is available.")
    }
    let old = try HelperCodeSignature.identity(source)
    let staged = try await stage(source, identity: old)
    try await Self.preflight(staged)
    let previous = job.route == .userAgent ? try? Data(contentsOf: fallback) : nil
    return HelperLaunchPlan(identity: old, route: .userAgent, plist: try plist(executable: staged, previous: previous))
  }

  private func stage(_ executable: URL, identity: HelperIdentity) async throws -> URL {
    let destination = home.appending(path: "helpers/\(identity.sha256)/limmr-engine")
    return try await Task.detached(priority: .utility) {
      let fm = FileManager.default
      try fm.createDirectory(at: destination.deletingLastPathComponent(), withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
      if !fm.fileExists(atPath: destination.path) {
        let temporary = destination.deletingLastPathComponent().appending(path: UUID().uuidString)
        defer { try? fm.removeItem(at: temporary) }
        try fm.copyItem(at: executable, to: temporary)
        try fm.setAttributes([.posixPermissions: 0o700], ofItemAtPath: temporary.path)
        try fm.moveItem(at: temporary, to: destination)
      }
      guard try HelperCodeSignature.identity(destination) == identity else { throw HelperUpdateFailure(reason: "The staged helper signature or file fingerprint does not match the bundled build.") }
      return destination
    }.value
  }

  static func preflight(_ executable: URL) async throws {
    try await Task.detached(priority: .utility) {
      let process = Process()
      process.executableURL = executable; process.arguments = ["-h"]
      process.standardInput = FileHandle.nullDevice
      process.standardOutput = FileHandle.nullDevice; process.standardError = FileHandle.nullDevice
      // -h exits in Go's flag parser, before any stores/listeners/WhatsApp initialization.
      let temporaryHome = FileManager.default.temporaryDirectory.appending(path: "limmr-helper-check-\(UUID().uuidString)")
      defer { try? FileManager.default.removeItem(at: temporaryHome) }
      process.environment = ["PATH": "/usr/bin:/bin", "HOME": FileManager.default.homeDirectoryForCurrentUser.path,
                             "LIMMR_HOME": temporaryHome.path]
      try process.run()
      let deadline = Date().addingTimeInterval(5)
      while process.isRunning && Date() < deadline { try await Task.sleep(for: .milliseconds(20)) }
      if process.isRunning { kill(process.processIdentifier, SIGKILL); await process.waitForExit(); throw HelperUpdateFailure(reason: "The helper help check timed out after five seconds.") }
      guard process.terminationReason == .exit, process.terminationStatus == 0 else {
        throw HelperUpdateFailure(reason: "The helper help check failed (exit status \(process.terminationStatus), \(process.terminationReason == .exit ? "exit" : "signal")).")
      }
    }.value
  }

  private func plist(executable: URL, previous: Data? = nil) throws -> Data {
    var content = previous.flatMap { try? PropertyListSerialization.propertyList(from: $0, format: nil) as? [String: Any] } ?? [:]
    var arguments = content["ProgramArguments"] as? [String] ?? [executable.path]
    if arguments.isEmpty { arguments = [executable.path] } else { arguments[0] = executable.path }
    content["Label"] = "dev.limmr.engine"; content["ProgramArguments"] = arguments
    content.removeValue(forKey: "BundleProgram"); content.removeValue(forKey: "Program")
    content["RunAtLoad"] = true; content["KeepAlive"] = true; content["ProcessType"] = "Background"
    if previous == nil { content["EnvironmentVariables"] = ["LIMMR_HOME": home.path] }
    return try PropertyListSerialization.data(fromPropertyList: content, format: .xml, options: 0)
  }

  func remove() async throws {
    try await HelperLaunchCleanup.remove(status: service.status,
      unregister: { try await self.service.unregister() },
      jobExists: { (try? await self.system.run("/bin/launchctl", ["print", self.jobName])) != nil },
      bootout: { _ = try await self.system.run("/bin/launchctl", ["bootout", self.jobName]) },
      fallback: fallback, liveIsUserAgent: { (try? await self.job())?.route == .userAgent })
  }

  func launch(_ plan: HelperLaunchPlan) async throws {
    if plan.route == .serviceManagement {
      guard try await identity() == plan.identity else { throw BackendError.invalidResponse }
      try service.register()
    } else {
      guard let plist = plan.plist else { throw BackendError.invalidResponse }
      try FileManager.default.createDirectory(at: fallback.deletingLastPathComponent(), withIntermediateDirectories: true)
      try plist.write(to: fallback, options: .atomic)
      try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: fallback.path)
      try await HelperLaunchCleanup.bootstrap(
        run: { _ = try await self.system.run("/bin/launchctl", ["bootstrap", self.domain, self.fallback.path]) },
        staleServiceJob: { (try? await self.job())?.route == .serviceManagement },
        bootout: { _ = try? await self.system.run("/bin/launchctl", ["bootout", self.jobName]) })
    }
  }
}

private extension Data {
  var hex: String { map { String(format: "%02x", $0) }.joined() }
}

/// Small effect boundary so removal of both registration forms is tested with synthetic jobs/plists.
@MainActor enum HelperLaunchCleanup {
  static func remove(status: SMAppService.Status, unregister: () async throws -> Void,
                     jobExists: () async -> Bool, bootout: () async throws -> Void, fallback: URL,
                     liveIsUserAgent: () async -> Bool = { false },
                     pause: (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) async throws {
    // .notFound can mean a stale registration after bundle replacement; don't skip it.
    // When the live job is the user agent (or a record still awaits approval, so it never ran), the
    // login-item record is not what runs. Unregistering it refused (error 144) and made smd re-submit
    // the broken login item, so leave it alone and remove only the exact launchd job.
    if status != .notRegistered && status != .requiresApproval, !(await liveIsUserAgent()) {
      do { try await unregister() }
      catch { if status != .notFound { throw error } } // orphaned SM record; still remove the exact launchd job
    }
    if await jobExists() {
      try await bootout()
      // bootout returns before launchd finishes tearing the job down; bootstrapping the new helper too early is
      // refused (live 19 Oct: engine down 16 s). Wait up to 5 s for the label to be free.
      for _ in 0..<25 { guard await jobExists() else { break }; try await pause(.milliseconds(200)) }
    }
    if FileManager.default.fileExists(atPath: fallback.path) { try FileManager.default.removeItem(at: fallback) }
  }

  /// smd can re-submit a stale login item under the same label just before the user agent bootstraps
  /// (live: "spawn failed", EX_CONFIG). Boot out only that service-management job and retry.
  static func bootstrap(run: () async throws -> Void, staleServiceJob: () async -> Bool, bootout: () async -> Void,
                        attempts: Int = 3, pause: (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) async throws {
    for attempt in 1...attempts {
      do { try await run(); return }
      catch {
        guard attempt < attempts else { throw error }
        // Only a re-submitted service-management job is ever booted out; a user agent still exiting just needs time.
        if await staleServiceJob() { await bootout() }
        try await pause(.seconds(1))
      }
    }
  }
}
