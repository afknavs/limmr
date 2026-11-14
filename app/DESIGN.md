# Limmr visual system — Paper (10 November 2026)

The approved `docs/design/n4-fable/` HTML, CSS and PNGs supersede the older N1/N2 styling.
Use `LimmrTheme.swift` for every Paper token, 12/14/17/20/24 type, 4 pt spacing,
16 pt filled borderless cards, 12 pt controls and a 24 pt page panel. Sidebar width is 232 pt.
Colour means state: orange square needs you, blue working/new, green connected.
No gradients, shadows or sparkle symbols in content. Faces use backend photos or neutral initials;
only real task covers are shown. Keep existing navigation, keyboard, accessibility and task rules.
Missing data is omitted, never replaced with invented numbers outside fixture mode.

Snapshot processes use an invented backend and throwaway LIMMR_HOME, never a helper or live ports.
Unbundled `.build/*/Limmr --snapshot` only. See `docs/lanes/n4-look.md` for the full contract.
