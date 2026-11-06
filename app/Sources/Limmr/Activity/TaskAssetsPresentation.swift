import Foundation
import LimmrKit

enum TaskAssetsPresentation {
  static func items(_ items: [WorkspaceItem], filter: String, query: String) -> [WorkspaceItem] {
    let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
    return items.filter { item in
      guard ["file", "link"].contains(item.kind) else { return false }
      let image = item.mediaType == "image"
      let matches = filter == "All" || filter == "Images" && image || filter == "Files" && item.kind == "file" && !image || filter == "Links" && item.kind == "link"
      let haystack = [item.aiName, item.title, item.ocrText, item.sender, item.chat, item.caption, item.description].compactMap { $0 }.joined(separator: " ")
      return matches && (query.isEmpty || haystack.localizedStandardContains(query))
    }.sorted {
      let a = rank($0), b = rank($1)
      if a != b { return a < b }
      if $0.addedAt != $1.addedAt { return $0.addedAt > $1.addedAt }
      return $0.id < $1.id
    }
  }
  static func rank(_ item: WorkspaceItem) -> Int {
    if item.mediaType == "image" {
      let text = [item.aiName, item.caption, item.description].compactMap { $0 }.joined(separator: " ").lowercased()
      return text.contains("poster") || text.contains("flyer") ? 0 : 1
    }
    if item.mediaType == "video" { return 2 }
    return item.kind == "file" ? 3 : 4
  }
}
