import Foundation
import LimmrKit
import Testing
@testable import Limmr

@Suite("N4 responsive geometry") struct N4ResponsiveTests {
  @Test func sidebarBreakpointsAndMinimumWindow() {
    #expect(WindowMetrics.minimumWidth == 900 && WindowMetrics.minimumHeight == 600)
    for (width, sidebar) in [(900.0,56.0),(959,56),(960,200),(1100,200),(1101,232)] {
      #expect(PaperLayout.sidebarWidth(width) == sidebar)
    }
    #expect(!PaperLayout.sideBySide(1099) && PaperLayout.sideBySide(1100))
  }

  @Test func cardsAndInspectorsRemainReadableAcrossTheCaptureMatrix() {
    let widths = [900.0,1000,1180,1280,1440,1728,1920]
    let columns = [2,2,3,3,4,5,5]
    for (window, count) in zip(widths, columns) {
      let content = PaperLayout.contentWidth(window)
      #expect(PaperLayout.taskColumns(window) == count)
      let card = (content - Double(count - 1) * 12) / Double(count)
      #expect(card >= 244 && card <= 400)
      // Pages fill the window: no reading cap, so every extra point of window is content.
      #expect(content == window - PaperLayout.sidebarWidth(window) - 64)
      let inspector = PaperLayout.inspectorWidth(window)
      #expect((300...360).contains(inspector))
      if PaperLayout.sideBySide(window) {
        #expect(PaperLayout.taskListWidth(window) >= 480)
        #expect(abs(PaperLayout.taskListWidth(window) + inspector + 12 - content) < 0.001)
      }
      let peopleColumns = PaperLayout.peopleColumns(window)
      #expect(peopleColumns == count - 1)
      let person = (content - Double(peopleColumns - 1) * 20) / Double(peopleColumns)
      #expect(person >= 300)
    }
    #expect([600.0,800,1100,1320].map { PaperLayout.peopleColumns($0) } == [1,1,2,2])
    #expect(PaperLayout.peopleColumns(1920) == 4)
    #expect(PaperLayout.contentWidth(1920) - PaperLayout.contentWidth(1728) == 192)
  }

  @Test func tallWindowsGiveHeightToThePrimaryContentWithoutACap() {
    for height in [600.0,700,760,800,900,1080,1100,1200] {
      #expect(PaperPeopleNetwork.mapHeight(windowHeight: height) == max(280, height - 338))
      #expect(PaperLayout.circleListHeight(windowWidth: 1280, windowHeight: height) == max(320, height - 232))
      #expect(PaperLayout.conversationHeight(height) >= 220)
    }
    #expect(PaperLayout.mapHeight(windowHeight: 1200) - PaperLayout.mapHeight(windowHeight: 900) == 300)
    #expect(PaperLayout.circleListHeight(windowWidth: 1000, windowHeight: 1100) > PaperLayout.circleListHeight(windowWidth: 1000, windowHeight: 700))
  }
}
