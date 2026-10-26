import Foundation

/// Only HTTPS redirects are accepted (GitHub redirects release assets to its CDN).
private final class UpdateRedirects: NSObject, URLSessionTaskDelegate, Sendable {
  func urlSession(_ session: URLSession, task: URLSessionTask,
                  willPerformHTTPRedirection response: HTTPURLResponse,
                  newRequest request: URLRequest) async -> URLRequest? {
    guard let url = request.url, secureUpdateURL(url) else { return nil }
    return request
  }
}

public actor UpdateChecker {
  private let session: URLSession
  public init(session: URLSession = URLSession(configuration: .ephemeral)) { self.session = session }

  public func fetch(feedURL: URL, publicKey: Data) async throws -> UpdateFeed {
    guard secureUpdateURL(feedURL) else { throw UpdateError.invalidFeed }
    var request = URLRequest(url: feedURL, cachePolicy: .reloadIgnoringLocalCacheData)
    request.timeoutInterval = 30
    let (bytes, response) = try await session.bytes(for: request, delegate: UpdateRedirects())
    guard let response = response as? HTTPURLResponse, response.statusCode == 200,
          response.expectedContentLength <= 262_144 else { throw UpdateError.unavailable }
    var data = Data()
    for try await byte in bytes {
      guard data.count < 262_144 else { throw UpdateError.tooLarge }
      data.append(byte)
    }
    return try UpdateFeed.verified(data, publicKey: publicKey)
  }

  public func download(_ feed: UpdateFeed, to destination: URL) async throws {
    guard secureUpdateURL(feed.url) else { throw UpdateError.invalidFeed }
    var request = URLRequest(url: feed.url, cachePolicy: .reloadIgnoringLocalCacheData)
    request.timeoutInterval = 300
    let (bytes, response) = try await session.bytes(for: request, delegate: UpdateRedirects())
    guard let response = response as? HTTPURLResponse, response.statusCode == 200,
          response.expectedContentLength < 0 || response.expectedContentLength == feed.size
    else { throw UpdateError.unavailable }
    guard FileManager.default.createFile(atPath: destination.path, contents: nil,
                                        attributes: [.posixPermissions: 0o600]) else { throw UpdateError.unavailable }
    let handle = try FileHandle(forWritingTo: destination)
    do {
      var buffer = Data(); var count: Int64 = 0
      for try await byte in bytes {
        count += 1
        guard count <= feed.size else { throw UpdateError.tooLarge }
        buffer.append(byte)
        if buffer.count == 65_536 { try handle.write(contentsOf: buffer); buffer.removeAll(keepingCapacity: true) }
      }
      try handle.write(contentsOf: buffer); try handle.close()
      try feed.verifyArchive(destination)
    } catch {
      try? handle.close(); try? FileManager.default.removeItem(at: destination)
      throw error
    }
  }
}
