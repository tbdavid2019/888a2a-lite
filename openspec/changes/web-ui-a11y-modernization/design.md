# Design: Web UI Accessibility & Modern Platform Modernization

## Context

See `proposal.md` for audit context. Both hosted HTML surfaces (`internal/service/chat.html` and `internal/service/admin.html`) are embedded via `//go:embed` in `internal/service/http.go` and served with strict CSP (`default-src 'none'; script-src 'nonce-...'`). Any new script must carry `nonce="{{CSP_NONCE}}"`, and `internal/service/http_test.go` asserts that zero unreplaced `{{CSP_NONCE}}` markers leak into rendered output while existing SEO/PWA markers, CJK IME composition guards, hashed Circle ID warnings, and 3-language i18n (`zh-TW`/`en`/`ja`) remain intact.

## Goals / Non-Goals

**Goals:**
- Achieve WCAG AA (>= 4.5:1) contrast across every solid button, pill, and selected popover item in Light and Dark themes on both `chat.html` and `admin.html`.
- Replace custom `<div>` modal overlays with native HTML `<dialog closedby="any">` + `showModal()` for real focus trapping, `inert` background containment, and top-layer stacking without adding third-party dependencies.
- Prevent Dark Mode FOUC under strict CSP nonces, respect `prefers-reduced-motion`, unify button icons using `14px` inline stroke-2 SVGs matching the topbar, streamline pre-connection onboarding, and add ARIA combobox bindings to `@` mentions.

**Non-Goals:**
- Adding external fonts, icon CDN packages, or build pipelines (HTML remains zero-dependency embedded static files).
- Refactoring desktop layout dimensions into rigid 4px-grid spacing rules or stripping functional middle-dot (`·`) separators that serve high-density `Operate`-mode scannability.
- Modifying Go Hub endpoints, SQLite tables, or Python `a2a_bridge.py`.

## Decisions

1. **CSP-Safe Early Theme Bootstrap (`<head>`)**:
   - Place a minimal inline `<script nonce="{{CSP_NONCE}}">` right before `</head>` (after `<meta name="color-scheme">`) reading `localStorage.getItem("a2a_ui_theme")` (same shared `THEME_KEY` across `chat.html` and `admin.html`) falling back to `matchMedia("(prefers-color-scheme: dark)").matches`, setting `document.documentElement.setAttribute("data-theme", theme)` synchronously before `<body>` parses.
   - Keep full theme toggle & `matchMedia` listener in the main bottom `<script>` to update button icons/ARIA labels and handle OS changes dynamically.
   - *Alternative considered*: inline `onclick` or un-nonced script — rejected because `script-src 'nonce-...'` blocks them and `http_test.go` verifies nonce matching.

2. **Native `<dialog closedby="any">` Migration in `chat.html`**:
   - Retain existing DOM IDs (`skills-modal-backdrop`, `resources-modal-backdrop`) directly on the `<dialog>` elements so DOM lookups (`$("skills-modal-backdrop")`) stay consistent, while wrapping inner dialog content in `.skills-modal-card` (padding `18px`, `border-radius: 14px`) and styling `<dialog>` itself as a full-bleed transparent/padding shell or native centered top-layer dialog (`background: transparent; border: none; padding: 16px; max-width: min(860px, calc(100vw - 32px)); max-height: 90dvh; margin: auto;`).
   - Actually styling `<dialog class="skills-dialog" closedby="any">` directly as the bordered modal box (`background: var(--surface); border: 1px solid var(--line-strong); border-radius: 14px; padding: 18px; width: min(860px, calc(100vw - 32px)); max-height: 90dvh; margin: auto; color: var(--ink);`) paired with `dialog.skills-dialog::backdrop { background: var(--backdrop-bg); overflow: hidden; overscroll-behavior: contain; }` and `dialog.skills-dialog:modal { overscroll-behavior: contain; }` is cleaner and avoids `display: flex` overriding `dialog` closed state (`dialog:not([open]) { display: none; }`!).
   - *CRITICAL CSS GOTCHA*: Never set `display: flex` unconditionally on `dialog` (which breaks native hide behavior when closed!); scope `dialog.skills-dialog[open] { display: flex; flex-direction: column; gap: 14px; }`.
   - *Safari Light-Dismiss Fallback*: Where `!('closedBy' in HTMLDialogElement.prototype)`, bind `pointerdown`/`pointerup` outside `dialog.getBoundingClientRect()` (or `event.target === dialog` coordinate check outside rect per `light-dismiss-a-dialog`) to call `dialog.close()` without swallowing text selection drags.
   - Replace `classList.contains("hidden")` checks with `dialog.open`, and rely on `dialog`'s native `close` event + `showModal()` auto-focus-return (while keeping `closeModals()` helper calling `.close()` cleanly).

3. **Mobile Sidebar Drawer Containment (`chat.html`)**:
   - At `<= 820px`, when `setMobileSidebarOpen(true)` opens `#sidebar`, set `$("main-pane").inert = true` (add `id="main-pane"` to `<main>`), disable tabbing to obscured composer/connect-strip, and move focus to `#btn-sidebar-close` or `#roster-search`; on close (via backdrop click, `Escape`, or selecting a peer/group on mobile), restore `inert = false` and return focus cleanly if closed via Escape/button. On desktop resize (`matchMedia("(min-width: 821px)")`), automatically reset drawer open state and ensure `main-pane.inert = false` so rotating/resizing never traps desktop users!

4. **Inline SVG Icons + i18n Span Preservation**:
   - In `chat.html` (`skill-mode-doc`, `skill-mode-npx`, `btn-skill-format-npx`, `res-mode-doc`, `res-mode-asset`, `btn-res-upload`) and `admin.html` (`skill-format-npx`), keep `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">...</svg>` as a sibling before `<span data-i18n="...">Localized Label</span>` inside `<button>`.
   - Strip leading emojis from `I18N` / `ADMIN_I18N` translation strings across `zh-TW`, `en`, and `ja`, and move `data-i18n` onto the `<span>` so switching languages preserves the crisp SVG icon.

5. **Pre-Connection Onboarding Consolidation (`chat.html`)**:
   - Add body/app state class `is-disconnected` (toggled whenever `session === null`, toggled off when connected). When `.app.is-disconnected`, hide `.composer-wrap` (impossible to type anyway) and suppress redundant `#messages-scroll .empty-state` while `#connect-strip` is visible, letting `.connect-box` center cleanly inside `.main-pane` without competing duplicate messages. Note: if the user clicks "切換身分 / Circle" while connected (`session !== null`), `.composer-wrap` & existing thread stay unaffected while `#connect-strip` slides open above as before.

6. **ARIA Combobox + Viewport Guard on `@` Mention Popover & Scroll Performance**:
   - Give `#composer-input` `role="combobox"`, `aria-autocomplete="list"`, `aria-haspopup="listbox"`, `aria-controls="mention-popover"`, `aria-expanded="false"`, and dynamic `aria-activedescendant`. Assign deterministic option IDs `mention-opt-0`, `mention-opt-1`, ... on rendered `.mention-option` buttons.
   - Before showing `#mention-popover`, compute available space above `#composer-input` (`inputRect.top`) and clamp `.mention-popover` `max-height: min(190px, calc(100dvh - 140px))` (already above composer, which prevents overflowing narrow mobile virtual keyboard viewports!).
   - Add `content-visibility: auto; contain-intrinsic-size: auto 72px;` on `.msg-row` inside `.messages-scroll`.

## Risks / Trade-offs

- **CSS `dialog` `[open]` specificity** → Putting `display: flex` on plain `.skills-dialog` would make closed dialogs permanently visible on page load. Mitigation: strictly scope layout display to `.skills-dialog[open]` and verify both `render`/`DOM` state.
- **Mobile drawer `inert` stuck on viewport resize** → Rotating tablet or resizing browser from 768px to 1200px while drawer is open could leave `.main-pane` inert. Mitigation: attach `matchMedia("(min-width: 821px)").addEventListener("change", ...)` calling `setMobileSidebarOpen(false)`.
- **i18n translation parity & `http_test.go` checks** → Moving `data-i18n` to child `<span>` and updating JS selectors must preserve 100% key parity across `zh-TW`/`en`/`ja` and pass `http_test.go` checks (`data-theme="dark"`, `imeComposing`, `lastCompositionEndAt`, `{{CSP_NONCE}}`, etc.).
