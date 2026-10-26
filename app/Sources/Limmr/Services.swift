import AppKit
import ServiceManagement
import UserNotifications
import LimmrKit
import Observation
import OSLog

enum LaunchAtLoginStatus: Equatable {
    case enabled, notRegistered, requiresApproval, notFound

    var isOn: Bool { self == .enabled || self == .requiresApproval }
    var detail: String {
        switch self {
        case .enabled: "On"
        case .notRegistered: "Off"
        case .requiresApproval: "Needs your OK in System Settings → Login Items"
        case .notFound: "Limmr couldn’t be found. Install it in Applications and try again."
        }
    }
}

@MainActor protocol LaunchAtLoginManaging {
    var status: LaunchAtLoginStatus { get }
    func register() throws
    func unregister() async throws
    func openLoginItems()
}

/// Only this adapter talks to ServiceManagement. Samples never register a real app.
@MainActor final class MainAppLoginService: LaunchAtLoginManaging {
    private let demo: Bool
    private var demoStatus: LaunchAtLoginStatus = .notRegistered
    init(demo: Bool) { self.demo = demo }
    var status: LaunchAtLoginStatus {
        if demo { return demoStatus }
        switch SMAppService.mainApp.status {
        case .enabled: return .enabled
        case .notRegistered: return .notRegistered
        case .requiresApproval: return .requiresApproval
        case .notFound: return .notFound
        @unknown default: return .notFound
        }
    }
    func register() throws {
        if demo { demoStatus = .enabled; return }
        try SMAppService.mainApp.register()
    }
    func unregister() async throws {
        if demo { demoStatus = .notRegistered; return }
        try await SMAppService.mainApp.unregister()
    }
    func openLoginItems() {
        guard !demo else { return }
        SMAppService.openSystemSettingsLoginItems()
    }
}

@MainActor @Observable final class LaunchAtLoginSetting {
    private let service: any LaunchAtLoginManaging
    private(set) var status: LaunchAtLoginStatus
    private(set) var isChanging = false
    private(set) var error: String?
    private var requested: Bool?
    @ObservationIgnored private var change: Task<Void, Never>?
    var isOn: Bool { requested ?? status.isOn }

    init(service: any LaunchAtLoginManaging) {
        self.service = service
        status = service.status
    }
    func refresh() {
        // unregister() suspends. A lookup during it may still return the previous state.
        guard !isChanging else { return }
        status = service.status
    }
    func setEnabled(_ enabled: Bool) {
        guard !isChanging else { return }
        requested = enabled
        isChanging = true
        error = nil
        // Independent of both the engine save queue and its availability.
        change = Task {
            defer {
                status = service.status
                requested = nil
                isChanging = false
            }
            do {
                if enabled { try service.register() }
                else { try await service.unregister() }
            } catch {
                // Never log localized errors, which can include local paths or other private data.
                let code = (error as NSError).code
                Logger(subsystem: "dev.limmr", category: "LaunchAtLogin")
                    .error("Launch at login change failed (enable: \(enabled), code: \(code))")
                self.error = "Couldn’t turn launch at login \(enabled ? "on" : "off"). Try again, or check System Settings → Login Items."
            }
        }
    }
    func waitForChange() async { await change?.value }
    func openLoginItems() { service.openLoginItems() }
}

struct RuntimePaths {
    let home: URL
    init() {
        if let override = ProcessInfo.processInfo.environment["LIMMR_HOME"] { home = URL(fileURLWithPath: override, isDirectory: true) }
        else { home = FileManager.default.homeDirectoryForCurrentUser.appending(path: "Library/Application Support/Limmr", directoryHint: .isDirectory) }
    }
    func token() throws -> String {
        try String(contentsOf: home.appending(path: "control-token"), encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
    }
}

/// System effects are disabled in demo and snapshot mode.
@MainActor final class SystemActions {
    let demo: Bool
    let login: LaunchAtLoginSetting
    let paths = RuntimePaths()
    let startup = EngineStartup()
    private lazy var helperDriver = LaunchdHelperDriver(system: self)
    private lazy var helperRegistration = HelperRegistration(driver: helperDriver)
    private var engineBuild: EngineBuildReceipt {
        EngineBuildReceipt(helper: Bundle.main.bundleURL.appending(path: "Contents/Helpers/limmr-engine"),
                           receipt: paths.home.appending(path: "registered-helper-sha256"))
    }
    init(demo: Bool, loginService: (any LaunchAtLoginManaging)? = nil) {
        self.demo = demo
        login = LaunchAtLoginSetting(service: loginService ?? MainAppLoginService(demo: demo))
    }
    func run(_ executable: String, _ arguments: [String]) async throws -> String {
        guard !demo else { return "Demo: no system changes were made." }
        return try await Task.detached {
            let process = Process()
            process.executableURL = URL(fileURLWithPath: executable)
            process.arguments = arguments
            process.standardInput = FileHandle.nullDevice
            let output = Pipe()
            process.standardOutput = output
            process.standardError = FileHandle.nullDevice
            try process.run()
            let data = output.fileHandleForReading.readDataToEndOfFile()
            await process.waitForExit()
            guard process.terminationStatus == 0 else { throw BackendError.unavailable("The command could not finish. Check that the application is installed and try again.") }
            return String(decoding: data, as: UTF8.self)
        }.value
    }
    func executable(_ name: String) -> String? {
        if name == "tailscale" {
            let bundled = "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
            if FileManager.default.isExecutableFile(atPath: bundled) { return bundled }
        }
        let directories = (ProcessInfo.processInfo.environment["PATH"] ?? "/usr/bin:/bin").split(separator: ":").map(String.init) + ["/opt/homebrew/bin", "/usr/local/bin"]
        return directories.map { $0 + "/" + name }.first { FileManager.default.isExecutableFile(atPath: $0) }
    }
    func installClaude(token: String) async throws {
        guard let executable = executable("claude") else { throw BackendError.unavailable("Install Claude Code first, then try again.") }
        _ = try await run(executable, ["mcp", "add", "--scope", "user", "--transport", "http", "limmr", "http://127.0.0.1:18800/mcp", "--header", "Authorization: Bearer \(token)"])
    }
    func installCodex(token: String) throws {
        guard !demo else { return }
        let config = FileManager.default.homeDirectoryForCurrentUser.appending(path: ".codex/config.toml")
        let exists = FileManager.default.fileExists(atPath: config.path)
        let previous = exists ? try String(contentsOf: config, encoding: .utf8) : ""
        guard !previous.contains("[mcp_servers.limmr]") else { throw BackendError.unavailable("Limmr already exists in the Codex configuration. Remove its old entry in Codex before reconnecting.") }
        if FileManager.default.fileExists(atPath: config.path) {
            try FileManager.default.copyItem(at: config, to: config.appendingPathExtension("limmr-backup-\(Int(Date().timeIntervalSince1970))"))
        }
        try FileManager.default.createDirectory(at: config.deletingLastPathComponent(), withIntermediateDirectories: true)
        // JSON string escaping is also valid TOML basic-string escaping for this ASCII credential.
        let encoded = String(decoding: try JSONEncoder().encode(token), as: UTF8.self)
        let block = "\n[mcp_servers.limmr]\nurl = \"http://127.0.0.1:18800/mcp\"\nhttp_headers = { Authorization = \"Bearer \(String(encoded.dropFirst().dropLast()))\" }\n"
        try (previous + block).write(to: config, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: config.path)
    }
    var engineRegistered: Bool { registration != .absent }
    func startEngine() async throws {
        guard !demo else { return }
        await startup.recover(using: self, allowInstall: true)
        guard startup.phase == .ready else {
            throw HelperUpdateFailure(reason: startup.failureMessage ?? "The background helper couldn’t start. Try again.")
        }
    }
    var registration: EngineRegistration {
        guard !demo else { return .absent }
        return helperDriver.registration
    }
    /// Short independent requests keep startup and pairing clear of heavy refresh work.
    func localData(_ path: String) async throws -> Data {
        guard !demo else { throw BackendError.invalidResponse }
        var request = URLRequest(url: URL(string: "http://127.0.0.1:18800" + path)!)
        request.timeoutInterval = 2
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.setValue("Bearer " + (try paths.token()), forHTTPHeaderField: "Authorization")
        let (data, response) = try await URLSession.shared.data(for: request, delegate: LocalProbeRedirects())
        let status = (response as? HTTPURLResponse)?.statusCode
        // R3 exposes authenticated startup progress as 503 until health is ready.
        // Keep that signal available to HelperReadinessWait instead of treating it as a dead helper.
        let startingHealth = path == "/v1/health" && status == 503
          && (try? Wire.decode(EngineStartingResponse.self, from: data))?.status == "starting"
        guard status == 200 || startingHealth else { throw BackendError.invalidResponse }
        return data
    }
    func healthReadiness() async -> HelperReadiness {
        guard let data = try? await localData("/v1/health") else { return .unavailable }
        if let progress = try? Wire.decode(EngineStartingResponse.self, from: data), progress.status == "starting" {
            return .starting(progress: progress.progressKey)
        }
        return (try? Wire.decode(HealthResponse.self, from: data)) != nil ? .ready : .unavailable
    }
    func readiness() async -> HelperReadiness { await helperDriver.readiness() }
    func probe() async -> Bool { await healthReadiness() == .ready }
    func needsUpdate() async throws -> Bool {
        try await helperRegistration.needsUpdate()
    }
    func recordHealthyHelper() async throws {
        guard await probe(), try await !needsUpdate() else { throw BackendError.invalidResponse }
        let build = engineBuild
        try await Task.detached(priority: .utility) { try build.record(build.fingerprint()) }.value
    }
    func unregister() async throws {
        guard !demo else { return }
        try await helperDriver.remove()
    }
    func register() async throws {
        guard !demo else { return }
        try await helperRegistration.register()
    }
    func stopEngine() async throws { try await unregister() }
    var isLaunchAtLoginEnabled: Bool { login.status == .enabled }
}

@MainActor final class NotificationBridge: NSObject, UNUserNotificationCenterDelegate {
    var navigate: ((URL) -> Void)?
    var resolve: ((String, Bool) -> Void)?
    private var seen = Set<String>()
    private let active: Bool
    init(active: Bool) { self.active = active; super.init() }
    func authorize() async throws {
        guard active else { return }
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        let approve = UNNotificationAction(identifier: "approve", title: "Approve")
        let deny = UNNotificationAction(identifier: "deny", title: "Deny", options: .destructive)
        center.setNotificationCategories([.init(identifier: "approval", actions: [approve, deny], intentIdentifiers: [])])
        _ = try await center.requestAuthorization(options: [.alert, .badge, .sound])
    }
    func update(_ approvals: [Approval]) {
        guard active else { return }
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        let pending = Set(approvals.filter(\.isPending).map(\.id))
        center.removeDeliveredNotifications(withIdentifiers: Array(seen.subtracting(pending)))
        for approval in approvals where approval.isPending && !seen.contains(approval.id) {
            let content = UNMutableNotificationContent()
            content.title = "A message is waiting for you"
            content.body = "Review the recipient and exact text in Limmr."
            content.categoryIdentifier = "approval"
            content.userInfo = ["approval_id": approval.id]
            center.add(.init(identifier: approval.id, content: content, trigger: nil))
        }
        seen.formUnion(pending)
    }
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        guard let id = response.notification.request.content.userInfo["approval_id"] as? String else { return }
        let action = response.actionIdentifier
        await MainActor.run {
            if action == "approve" || action == "deny" { resolve?(id, action == "approve") }
            else if let url = URL(string: "limmr://approvals/\(id)") { navigate?(url) }
        }
    }
}


extension SystemActions: EngineServiceManaging {}

private final class LocalProbeRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask,
                    willPerformHTTPRedirection response: HTTPURLResponse, newRequest request: URLRequest,
                    completionHandler: @escaping @Sendable (URLRequest?) -> Void) { completionHandler(nil) }
}

extension Process {
    /// Polls instead of waitUntilExit(), which spins the calling thread's run loop and can hang on a detached task.
    func waitForExit() async {
        while isRunning { try? await Task.sleep(for: .milliseconds(20)) }
    }
}
