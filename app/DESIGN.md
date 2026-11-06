# Limmr visual system (N1, revised 1 Nov 2026: native first)

The first N1 mockups (`docs/design/n1-identity/`) were rejected as "generic, made by AI": custom colours, a custom
sidebar and search box, a multicolour AI glow, a logo in the sidebar and flat icons. Keep their **layout ideas the
owner liked** (task cover tiles, the typographic cover when a task has no image, circle tiles, the sidebar's
sections, the activity squares) and build them **from Apple's own components** so Limmr looks like a Mac app.

Rules distilled from Apple's HIG and four MIT-licensed skills; read them before building (read with
`gh api repos/<repo>/contents/<path> -H "Accept: application/vnd.github.raw"`; do not install or save them):
`ehmo/platform-design-skills` `skills/macos/SKILL.md` (sections 3, 4, 6, 9, 11);
`rshankras/claude-code-apple-skills` `skills/design/liquid-glass/SKILL.md`, `skills/design/sf-symbols/SKILL.md`,
`skills/design/typography/SKILL.md`; `Dimillian/Skills` `swiftui-liquid-glass/SKILL.md`;
`AvdLee/SwiftUI-Agent-Skill` `skills/swiftui-expert-skill/SKILL.md` (macOS and performance parts).

## Principles

1. **Native before custom.** `NavigationSplitView` with a `.listStyle(.sidebar)` source list (system vibrancy),
   a unified toolbar, `.searchable` in the toolbar, `Picker(.segmented)` for view switching, `.badge()` for counts,
   system context menus, popovers, Quick Look. If a system control exists, use it.
2. **System colour.** Semantic colours only (`.primary`, `.secondary`, `.tertiary`, `Color(nsColor: .windowBackgroundColor)`,
   `.quaternary` fills, `.separator`). The **system accent colour** for selection and actions (never a fixed brand
   blue). Amber (`.orange`) only for "needs you". Circle hues are the only custom colours, and only on circle
   symbols and covers.
3. **System type.** Text styles, never fixed point sizes: `.largeTitle` page titles only where macOS uses them,
   `.title2`/`.title3` sections, `.headline` card titles, `.body`, `.callout`, `.subheadline`, `.caption` meta.
   Weight and colour make hierarchy, not size jumps.
4. **SF Symbols everywhere**, hierarchical rendering, `.imageScale` matched to the text beside it. Symbol effects
   (`.bounce`, `.pulse`) only for real state changes.
5. **Liquid Glass only where Apple puts it**: the toolbar and sidebar (automatic), floating controls, the hover
   label on a cover tile, popovers. Never on content cards, never as decoration. Use `.glassEffect()` /
   `GlassEffectContainer`, respect Reduce Transparency with a solid fallback.
6. **No AI glow, no sparkle badges.** AI-written text is marked by a plain "by Claude Code · 5 min" caption.
7. **No logo inside the window.** The app icon lives in the Dock and the About window.
8. **Use the window, no empty boxes.** 20 pt margins, 8 pt between related items, 20 pt between groups. Content
   fills the window (up to about 1320 pt); hidden sections leave no placeholder.
9. **Mac behaviour**: hover states on everything clickable, right-click menus on every item, keyboard navigation
   (arrows, Return opens, Space = Quick Look on media), ⌘F search, Escape closes, VoiceOver labels.

## Structure

- **Sidebar** (source list): Home, Tasks (`.badge` = needs-you count), People, Activity; a "Circles" section, each
  row a `Label` with the circle's symbol tinted its hue; a "Your AIs" section at the bottom, one row per AI with a
  small presence dot and last-seen time, clicking opens it in Settings › Integrations. A dot and the AI running on it
  are one row ("Dot · Pip"). This replaces the three unlabeled bottom-left icons.
- **Toolbar** per screen: title, `.searchable` (trailing), and screen controls. Tasks: a segmented
  Gallery | List picker and a circle filter menu.
- **Home**: a plain "Needs you" list first (native rows: approvals, drafts waiting, tasks marked needs you; each opens
  its target; hidden when empty), then "Recently" as at most three plain rows of what moved, then the Tasks cover
  row, then Circles tiles, and a right column (≥1200 pt) with "What your AIs did" and the activity squares
  (last 12 weeks, GitHub/Claude-usage style, accent colour at four opacities). The full 42-week squares live on
  Activity.
- **Tasks**: no overview card. Gallery: adaptive grid of 4:5 cover tiles (min 200 pt). The cover fills the tile;
  title, one-line status and circle appear on hover/focus on a small glass label at the bottom. Needs-you: a small
  orange dot. No image: typographic cover (the circle symbol in its hue, the title in `.title2` bold, circle · due)
  on a quiet fill. List: a native `Table` (thumbnail, title + status, circle, progress, AI working on it, last
  moved) with sortable columns, grouped Needs you / Active, then Quiet and History as disclosure groups.
- **Task page / circle page**: native sections; media tiles show image only, name on hover; Space opens Quick Look.

## Circle hues

Stable per circle (hash of id) unless the owner picks: system `.blue`, `.orange`, `.purple`, `.green`, `.yellow`,
`.teal`, `.pink`, `.indigo`. Use them for the circle symbol and typographic cover tint only.

## Kept from earlier rounds

The main window starts at 1100×740 and stays usable at 800×600 (minimum 760×520); scroll rather than compress.
Failures stay beside the action in plain words. Unknown delivery outcomes never retry sends. Demo data is invented
and in-memory; snapshot runs use a throwaway LIMMR_HOME and never start a helper. Person avatars load through the
authenticated backend; a missed image uses a deterministic initial tile. The people map is one accessibility
summary and the lists remain the accessible navigation path.

## N2 shell adjustments (owner review, 1 November)

N2 supersedes the N1 hover-label, Home-rail calendar and circle-colour treatments above. Task titles,
circles and one-line status stay below the cover. Content cards use neutral hover/press feedback and a
thin secondary keyboard outline with native focus effects disabled. Circle symbols in the sidebar and
Home are monochrome; Home uses available circle/group picture pixels without a tinted tile background.
Home starts with Recently and Needs you, then the activity calendar and its last-30-day counts, Tasks,
and Circles. AI highlights sit beside the main content when space permits, or below it. Retained history
remains visible while WhatsApp is unlinked. Circle hue is reserved for small distinguishing accents.
