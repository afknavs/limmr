import Foundation

public protocol Backend: Sendable {
    func request(_ path: String, method: String, body: Data?) async throws -> Data
    func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error>
}
public extension Backend {
    func get<T: Decodable & Sendable>(_ path: String, as: T.Type = T.self) async throws -> T {
        let data = try await request(path, method: "GET", body: nil)
        return try await Task.detached(priority: .userInitiated) { try Wire.decode(T.self, from: data, route: path) }.value
    }
    func post(_ path: String) async throws {
        _ = try await request(path, method: "POST", body: nil)
    }
    func post<T: Encodable & Sendable>(_ path: String, _ body: T) async throws {
        _ = try await request(path, method: "POST", body: Wire.data(body))
    }
}

public struct SSEEvent: Equatable, Sendable {
    public var event: String
    public var data: String
    public var id: String?
}
/// Incremental parser: comments, CRLF, multi-line data, event ids and empty events.
public struct SSEParser: Sendable {
    private var event = "message"
    private var lines: [String] = []
    private var id: String?
    public init() {}
    public mutating func consume(_ raw: String) -> SSEEvent? {
        let line = raw.hasSuffix("\r") ? String(raw.dropLast()) : raw
        if line.isEmpty {
            defer { lines = []; event = "message" }
            guard !lines.isEmpty else { return nil }
            return SSEEvent(event: event, data: lines.joined(separator: "\n"), id: id)
        }
        if line.hasPrefix(":") { return nil }
        let parts = line.split(separator: ":", maxSplits: 1, omittingEmptySubsequences: false)
        let value = parts.count > 1 ? String(parts[1]).replacingOccurrences(of: "^ ", with: "", options: .regularExpression) : ""
        switch parts[0] {
        case "data": lines.append(value)
        case "event": event = value
        case "id": if !value.contains("\0") { id = value }
        default: break
        }
        return nil
    }
}

public final class APIClient: Backend, Sendable {
    public let baseURL: URL
    private let token: String
    private let credentialFile: URL?
    private let session: URLSession
    public init(baseURL: URL = URL(string: "http://127.0.0.1:18800")!, token: String, session: URLSession = .shared) {
        self.baseURL = baseURL; self.token = token; self.session = session; credentialFile = nil
    }
    public init(credentialFile: URL, baseURL: URL = URL(string: "http://127.0.0.1:18800")!, session: URLSession = .shared) {
        self.baseURL = baseURL; self.token = ""; self.session = session; self.credentialFile = credentialFile
    }
    private func urlRequest(_ path: String, method: String, body: Data?) throws -> URLRequest {
        guard baseURL.scheme == "http", ["127.0.0.1", "localhost"].contains(baseURL.host ?? ""),
              baseURL.user == nil, baseURL.password == nil,
              path.hasPrefix("/v1/"), let url = URL(string: path, relativeTo: baseURL)?.absoluteURL,
              url.host == baseURL.host, url.port == baseURL.port, url.fragment == nil,
              url.path.hasPrefix("/v1/") else { throw BackendError.invalidResponse }
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData)
        request.httpMethod = method; request.httpBody = body; request.timeoutInterval = 20
        let credential = try credentialFile.map { try String(contentsOf: $0, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines) } ?? token
        request.setValue("Bearer \(credential)", forHTTPHeaderField: "Authorization")
        request.setValue(path.hasSuffix("/avatar") ? "image/jpeg" : "application/json", forHTTPHeaderField: "Accept")
        if body != nil { request.setValue("application/json", forHTTPHeaderField: "Content-Type") }
        return request
    }
    public func request(_ path: String, method: String, body: Data?) async throws -> Data {
        let (data, response) = try await session.data(for: urlRequest(path, method: method, body: body), delegate: RejectRedirects())
        guard let http = response as? HTTPURLResponse else { throw BackendError.invalidResponse }
        if [200, 503].contains(http.statusCode),
           let progress = try? Wire.decode(EngineStartingResponse.self, from: data), progress.status == "starting" {
            throw BackendError.starting(progress: progress.progressKey)
        }
        guard (200..<300).contains(http.statusCode) else { throw BackendError.http(http.statusCode) }
        return data
    }
    public func events(_ path: String) -> AsyncThrowingStream<SSEEvent, Error> {
        AsyncThrowingStream { continuation in
            let task = Task {
                do {
                    var request = try urlRequest(path, method: "GET", body: nil)
                    request.timeoutInterval = 86_400
                    request.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                    let (bytes, response) = try await session.bytes(for: request, delegate: RejectRedirects())
                    guard let http = response as? HTTPURLResponse else { throw BackendError.invalidResponse }
                    guard (200..<300).contains(http.statusCode) else { throw BackendError.http(http.statusCode) }
                    guard http.mimeType == "text/event-stream" else { throw BackendError.invalidResponse }
                    var parser = SSEParser()
                    // `bytes.lines` skips empty lines, but an empty line is what ends an SSE event.
                    var buffer = Data()
                    for try await byte in bytes {
                        guard byte == UInt8(ascii: "\n") else { buffer.append(byte); continue }
                        try Task.checkCancellation()
                        let line = String(decoding: buffer, as: UTF8.self)
                        buffer.removeAll(keepingCapacity: true)
                        if let event = parser.consume(line) { continuation.yield(event) }
                    }
                    continuation.finish()
                } catch { continuation.finish(throwing: error) }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}

public enum DeepLink: Equatable, Sendable {
    case approvals(String?), agent(String), person(String), health
    public init?(url: URL) {
        guard url.scheme == "limmr" else { return nil }
        let value = url.pathComponents.dropFirst().first
        switch url.host {
        case "approvals": self = .approvals(value)
        case "agents": guard let value else { return nil }; self = .agent(value)
        case "people": guard let value else { return nil }; self = .person(value)
        case "health": self = .health
        default: return nil
        }
    }
}

/// Never forward the control credential to a redirect target.
private final class RejectRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}

public struct CallFailure: Error, LocalizedError, Sendable {
  public let path: String
  public let reason: String
  public init(path: String, cause: any Error) {
    self.path = path
    if let error = cause as? BackendError { reason = error.localizedDescription }
    else if cause is DecodingError { reason = "The response did not match the expected format." }
    else if let error = cause as? URLError { reason = error.code == .timedOut ? "The request timed out." : "The connection failed." }
    else { reason = "The request failed." }
  }
  public var errorDescription: String? { "GET \(path.components(separatedBy: "?")[0]): \(reason)" }
}
