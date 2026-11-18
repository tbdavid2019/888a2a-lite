# Tasks

## 1. Tokens, Contrast, Motion & Zero-FOUC Theme Init (`chat.html` & `admin.html`)

- [x] 1.1 Update `internal/service/admin.html` `:root` and `:root[data-theme="dark"]` tokens to introduce `--accent-btn-bg` (`#146c94`), `--accent-btn-hover` (`#0f5677` light / `#1674a0` dark), `--accent-btn-fg` (`#ffffff`), and `--focus-ring`, replacing literal colors on focus outlines/buttons and ensuring WCAG AA >= 4.5:1 button contrast (`sloplint` & manual ratio check pass).
- [x] 1.2 Update `internal/service/chat.html` Dark Mode `--accent-btn-hover` (`#1674a0` / >= 4.5:1), `.pill.accent` & `.mention-option.selected` text contrast (>= 4.5:1), lift `.skill-badge` literals into tokens, and add `overflow-x: clip` on `html, body` in both `chat.html` and `admin.html` (verified via CSS token audit & `impeccable detect --json`).
- [x] 1.3 Add CSP-nonced `<script nonce="{{CSP_NONCE}}">` early theme initializer in `<head>` of both `chat.html` and `admin.html` after `<meta name="color-scheme">` so saved `a2a_ui_theme` prevents Dark Mode FOUC, add `<meta name="description">` and `text-wrap: balance` on headings in `admin.html`, and verify `node --check` passes across all `<script>` blocks.
- [x] 1.4 Add `@media (prefers-reduced-motion: reduce)` transition/scroll suppression and tactile `button:active:not(:disabled)` scale feedback in both `chat.html` and `admin.html` (verified via `node --check` and live Chromium inspect).

## 2. Native Top-Layer `<dialog>`, Mobile Drawer Containment & Onboarding (`chat.html`)

- [x] 2.1 Migrate `#skills-modal-backdrop` and `#resources-modal-backdrop` in `internal/service/chat.html` to `<dialog closedby="any" class="skills-dialog">` opened via `showModal()` and styled under `dialog.skills-dialog[open]` + `::backdrop` with `overscroll-behavior: contain` and Safari coordinate light-dismiss fallback; update `closeModals()`, `openSkillsModal()`, `openResourcesModal()`, and `.open` checks (verified via `node --check` and browser Tab/Esc/backdrop test).
- [x] 2.2 Harden mobile sidebar drawer (`<= 820px`) in `internal/service/chat.html` by adding `id="main-pane"` to `<main>`, toggling `$("main-pane").inert = open`, moving focus into `#sidebar` on open, handling `Escape` and `matchMedia("(min-width: 821px)")` auto-reset on resize (verified via mobile viewport Tab/Esc check).
- [x] 2.3 Consolidate pre-connection onboarding in `internal/service/chat.html` via `.app.is-disconnected` state hiding the inactive `.composer-wrap` and redundant `.empty-state` while disconnected so `#connect-strip` is the single entry surface, restoring the workspace cleanly once connected (verified via desktop & mobile disconnected/connected renders).

## 3. Unified SVG Iconography, `@` Combobox ARIA & Scroll Optimization

- [x] 3.1 Replace OS emoji prefixes inside mode/action buttons in `internal/service/chat.html` (`skill-mode-doc`, `skill-mode-npx`, `btn-skill-format-npx`, `res-mode-doc`, `res-mode-asset`, `btn-res-upload`) and `internal/service/admin.html` (`skill-format-npx`) with `14px` inline stroke-2 SVGs paired with `<span data-i18n="...">`, strip leading emojis from `I18N` / `ADMIN_I18N` (`zh-TW`/`en`/`ja`), and verify 100% 3-language i18n key parity via Node script.
- [x] 3.2 Add combobox ARIA attributes (`role="combobox"`, `aria-autocomplete="list"`, `aria-haspopup="listbox"`, `aria-controls="mention-popover"`, dynamic `aria-expanded` and `aria-activedescendant`) + viewport-clamped height on `#mention-popover`, and add `content-visibility: auto; contain-intrinsic-size: auto 72px;` on `.msg-row` in `internal/service/chat.html` (verified via `node --check`).

## 4. Regression Tests, Changelog, CI Validation & Live Verification

- [x] 4.1 Extend `internal/service/http_test.go` assertions to verify `<dialog id="skills-modal-backdrop"`, `<dialog id="resources-modal-backdrop"`, `<meta name="description"` on `/admin/chat`, `prefers-reduced-motion`, and zero unreplaced `{{CSP_NONCE}}` across early `<head>` and bottom `<script>` tags.
- [ ] 4.2 Update `CHANGELOG.md`, run local static checks (`node --check`, i18n 3-lang parity, `impeccable detect --json`, `python3 -m unittest`), commit and push to verify GitHub Actions `CI` (`go test ./...`, `golangci-lint`, Python tests) & `Publish Docker image`, then deploy to `dns.glsoft.ai` and run live HTTPS + Chromium/mobile smoke checks.

## Workflow follow-up

- Archive `web-ui-a11y-modernization` into `openspec/specs/hosted-web-ui-accessibility/` after implementation and live verification complete.
