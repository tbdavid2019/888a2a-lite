# Proposal: Web UI Accessibility & Modern Platform Modernization

## Why

A cross-skill audit (`impeccable`, `modern-web-guidance`, `design-taste-frontend`, and `hallmark`) of the hosted browser workstations (`internal/service/chat.html` and `internal/service/admin.html`) uncovered concrete WCAG AA contrast gaps in Dark Mode, missing keyboard focus trapping in custom `<div>` modals, unguarded background content behind the mobile sidebar drawer, Dark Mode initial-paint flashing (FOUC), missing `prefers-reduced-motion` safeguards, duplicated pre-connection empty states, and mixed OS emoji vs SVG button iconography. Upgrading these surfaces to modern baseline web platform primitives improves accessibility, keyboard/IME usability, and visual craft without altering Hub APIs or breaking CSP nonces.

## What Changes

- **WCAG AA Dark-Mode Contrast & Tokenization (`chat.html`, `admin.html`)**: Split solid button colors in `admin.html` into `--accent-btn-bg` / `--accent-btn-hover` / `--accent-btn-fg` (>=4.5:1 contrast), refine `chat.html` Dark Mode button hover and `.pill.accent` / `.mention-option.selected` contrast, lift hard-coded `.skill-badge` and focus ring literals into `:root` tokens, add `overflow-x: clip` on `html, body`, and add `<meta name="description">` + `text-wrap: balance` on headings in `admin.html`.
- **Native Top-Layer `<dialog>` Modals (`chat.html`)**: Migrate `#skills-modal-backdrop` and `#resources-modal-backdrop` from custom `<div>` overlays to semantic `<dialog closedby="any">` opened via `showModal()`, adding pointer-coordinate backdrop click fallback for Safari and `overscroll-behavior: contain` on `dialog:modal` / `dialog::backdrop`.
- **Mobile Navigation Drawer Containment (`chat.html`)**: Set `inert` on the background workspace pane while the `< 820px` sidebar drawer is open, move focus into the drawer on open, and restore focus to `#btn-mobile-menu` on dismiss (`Esc`, backdrop tap, or close).
- **Zero-FOUC Theme Init, Motion & Tactile Feedback (`chat.html`, `admin.html`)**: Hoist synchronous theme detection (`localStorage` + `prefers-color-scheme`) into a CSP-nonced `<head>` `<script nonce="{{CSP_NONCE}}">`, add `@media (prefers-reduced-motion: reduce)` fallbacks, and add subtle `:active` press feedback (`scale(0.98)`).
- **Onboarding Stage Consolidation (`chat.html`)**: While disconnected (`session == null`), hide the redundant `.empty-state` card and disabled bottom composer so `#connect-strip` stands as the single clear entry task, revealing the conversation stage upon connection.
- **Unified Linear SVG Icons & Combobox/Scroll Polish (`chat.html`, `admin.html`)**: Replace OS emoji characters inside action buttons (`📄`, `📦`, `✨`, `📝`, `📎`, `☁️`) with consistent `14px` `stroke-width="2"` inline SVGs matching topbar iconography; wire `aria-controls`, `aria-expanded`, and `aria-activedescendant` + viewport flip guard on `#mention-popover`; add `content-visibility: auto; contain-intrinsic-size: auto 72px;` on `.msg-row`.

## Capabilities

### New Capabilities
- `hosted-web-ui-accessibility`: Accessibility, native `<dialog>` top-layer overlay semantics, zero-FOUC theme initialization, motion preference handling, onboarding clarity, and icon/token consistency across Hub-hosted HTML interfaces (`chat.html` and `admin.html`).

### Modified Capabilities
<!-- No existing Hub API, storage, Circle isolation, or functional workflow requirements change. -->

## Impact

- **Files**: `internal/service/chat.html`, `internal/service/admin.html`, `internal/service/http_test.go`, `CHANGELOG.md`.
- **APIs / Storage / CSP**: Zero backend Go or SQLite schema changes; strict `default-src 'none'; script-src 'nonce-...'` CSP remains intact with all scripts bearing `nonce="{{CSP_NONCE}}"`.
- **Local Verification**: `node --check`, 3-language i18n key parity checks (`zh-TW`/`en`/`ja`), and `impeccable detect --json` on both files; full `go test ./...` and `golangci-lint` validated via GitHub Actions before `dns.glsoft.ai` deployment.
