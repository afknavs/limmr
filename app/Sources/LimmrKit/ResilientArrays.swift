import Foundation
import OSLog

extension CodingUserInfoKey {
  static let wireStrictLists = CodingUserInfoKey(rawValue: "limmr.strictLists")!
  static let wireRoute = CodingUserInfoKey(rawValue: "limmr.route")!
}

/// Advance one complete value before attempting to decode it, so malformed
/// records never discard their readable neighbors. No payloads are logged.
private struct ReadableArray<Element: Decodable>: Decodable {
  var values: [Element] = []
  init(from decoder: any Decoder) throws {
    var container = try decoder.unkeyedContainer()
    var skipped = 0
    while !container.isAtEnd {
      let item = try container.superDecoder()
      do { values.append(try Element(from: item)) }
      catch {
        if decoder.userInfo[.wireStrictLists] as? Bool == true { throw error }
        skipped += 1
      }
    }
    if skipped > 0 {
      let route = decoder.userInfo[.wireRoute] as? String ?? "model"
      Logger(subsystem: "dev.limmr", category: "decode").warning("\(route, privacy: .public) skipped \(skipped) unreadable items")
    }
  }
}

extension KeyedDecodingContainer {
  // Synthesized and hand-written LimmrKit decoders use these concrete Array
  // overloads. Required scalar fields remain strict.
  public func decode<Element: Decodable>(_ type: [Element].Type, forKey key: Key) throws -> [Element] {
    guard contains(key), try !decodeNil(forKey: key) else { return [] }
    return try decode(ReadableArray<Element>.self, forKey: key).values
  }
  public func decodeIfPresent<Element: Decodable>(_ type: [Element].Type, forKey key: Key) throws -> [Element]? {
    guard contains(key), try !decodeNil(forKey: key) else { return nil }
    return try decode(ReadableArray<Element>.self, forKey: key).values
  }
}
