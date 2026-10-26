# Limmr visual system

Limmr is a quiet workspace for the people and circles in your life. The sidebar has Home, People and
Activity. Settings lives in the app menu and Command-comma. The A5 owner feedback supersedes the old map-led
Circles design.

- Use native SF typography: large bold page titles, body for messages, secondary callout section headers and
  caption metadata. Titles have at most one functional subtitle. Keep one alignment grammar throughout.
- Content is one readable column, maximum 760 points, centred on the system window background with 28-point
  margins. Blocks have 28-point separation; rows use hairlines or 8-point gaps and a subtle hover highlight.
  Compact circle tiles flow into two or three columns. No heavy cards, shadows, gradients or glows.
- Surfaces adapt to light and dark. Native sidebar translucency belongs to NavigationSplitView. Glass belongs to
  floating controls, toolbars and watch popovers. In print snapshots native material has a neutral system fallback; native source-list selections use equivalent plain rows because AppKit print capture cannot composite their selection backdrop.
  SF Symbols are secondary. Accent means live updates or an action needing you; no red or alarm glyphs.
- Home follows this order, omitting empty blocks: greeting and connection; watches (four plus overflow); queued
  drafts; pinned circles followed by at most three live circles; needs a reply; six recent activities; circle
  proposals; quiet integration footer. Quiet unpinned circles stay in People.
- Queued drafts are distinct from unanswered asks. Each draft has a compact variation switcher,
  one-click body editing with the caret at the end, and Send. The exact body text is posted and delivered unchanged. Edits are retained per variation. Swipe left reveals Dismiss; a full
  swipe dismisses. Context menu and accessibility action provide equivalent access.
- People opens with a 240-point translucent map of understanding. Seeded packing retains positions across
  refreshes; circles are labelled hubs, people are weighted dots, chats are small squares. Hairlines mean chat
  structure, accent lines mean AI discoveries, primary lines mean owner confirmation, dashes mean suggestions.
  Hover explains roles and provenance; clicking filters the list, clicking a person again opens their page.
  Event IDs drive new connection draw-in and short attributed capsules. Replay follows the last fourteen days
  in eight seconds. Reduce Motion uses static nodes and discrete replay steps. The map is one accessibility
  summary; the lists remain the accessible navigation path. Remembered Circles / People tabs share search.
- AI presence uses the same state on Home, the map and the sidebar. Idle agents do not add a Home line.
  Person avatars load through the authenticated backend; a missed image uses a deterministic muted initial tile
  with the same dark ink and light tint in either appearance. Sample mode uses no profile photos.
- Circle and person pages push in the same navigation stack at every size. They use blocks, preserving brief
  history, topics, per-circle roles and notes, chats, integration visibility and header send-rule controls. Pin
  is available in circle headers and tile context menus. Person pages include recent direct-message counts.
- Settings is a sidebar-list window with General, Integrations, Hidden chats, Remote access, Health and Advanced.
  Panes use grouped forms. Integration rows show an icon, name, visible-chat count and chevron to permissions.
  Configuration, credentials and ports remain behind Details. Changes save immediately with readback feedback.
- The main window starts at 1100×740 and remains usable at 800×600 (minimum 760×520). Scroll content rather than
  compressing text. Failures stay beside the action with plain words. Unknown delivery outcomes never retry sends.
- Demo data is invented and in-memory. Snapshot runs use a throwaway LIMMR_HOME and never start a helper.
  `scripts/snapshots-r1.sh` renders Home, People, a person page, Integrations and setup at 1100×740 in
  light and dark to `build/snapshots/r1/`. The app icon is a thin ring with two ink dots and one blue dot
  at 10, 2 and 6 o'clock on a softly shaded neutral tile.
  `scripts/snapshots-a6.sh` exports the five A6 views at both sizes and appearances to
  `build/snapshots/a6/` and refreshes `docs/images/people.png`. `scripts/snapshots.sh` exports all eight A5 views at 1100×740 and 800×600, light and dark, to
  `build/snapshots/a5/`, then refreshes hero, people and circle in `docs/images/` at 1280×800. The real watch opens
  an anchored native popover; print capture stages the same card on a floating, scrim-separated surface.
