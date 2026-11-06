import AppKit
import Foundation
import LimmrKit
import SwiftUI

struct TaskCoverSurface: ViewModifier {
  let preview: URL?
  func body(content: Content) -> some View {
    content.background {
      if let preview {
        GeometryReader { geometry in
          ThumbnailImage(url: preview).scaledToFill().frame(width: geometry.size.width, height: geometry.size.height).clipped()
            .overlay(LinearGradient(colors: [.black.opacity(0.48), .black.opacity(0.88)], startPoint: .top, endPoint: .bottom))
        }
      }
    }.clipShape(RoundedRectangle(cornerRadius: 10)).modifier(HomeTileSurface())
  }
}

struct TaskCoverPicker: View {
  @Bindable var store: AppStore
  let taskID: String
  @Environment(\.dismiss) private var dismiss
  @State private var task: TrackedTask?
  @State private var error: String?
  var body: some View {
    VStack(alignment: .leading, spacing: 16) {
      HStack { Text("Change cover").font(.title2.bold()); Spacer(); Button("Done") { dismiss() }.buttonStyle(.bordered) }
      ScrollView {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 180))], spacing: 10) {
          ForEach((task?.items ?? []).filter { $0.mediaType == "image" }) { item in
            CoverChoice(backend: store.backend, taskID: taskID, item: item) { choose(item.id) }
          }
        }
      }
      if let task, !task.items.contains(where: { $0.mediaType == "image" }) { Text("No images in this task yet.").foregroundStyle(AccessibleSecondary()) }
      if let error { Text(error).font(.callout) }
    }.padding(24).frame(width: 580, height: 420)
      .task { do { task = try await TaskClient(backend: store.backend).get(taskID) } catch { self.error = "Couldn’t load images." } }
  }
  private func choose(_ id: String) {
    Task {
      var patch = TaskPatch(); patch.coverItemId = id
      do { _ = try await TaskClient(backend: store.backend).patch(taskID, patch); await store.refreshTaskHome(); dismiss() }
      catch { self.error = "Couldn’t save the cover." }
    }
  }
}
private struct CoverChoice: View {
  let backend: any Backend
  let taskID: String
  let item: WorkspaceItem
  let choose: () -> Void
  @State private var preview: URL?
  var body: some View {
    Button(action: choose) {
      VStack(alignment: .leading, spacing: 6) {
        ZStack {
          RoundedRectangle(cornerRadius: 8).fill(.primary.opacity(0.04))
          if let preview { ThumbnailImage(url: preview).scaledToFill() }
          else { Image(systemName: "photo").font(.title).foregroundStyle(.secondary) }
        }.frame(height: 120).clipped().clipShape(RoundedRectangle(cornerRadius: 8))
        Text(item.aiName ?? item.title).font(.callout).lineLimit(2)
      }
    }.quietCard().task { preview = await TaskAssetSource(backend: backend, taskID: taskID, item: item).preview() }
  }
}
