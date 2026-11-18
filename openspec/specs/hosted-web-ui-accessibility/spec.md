# hosted-web-ui-accessibility Specification

## Purpose

Defines accessibility, platform-native overlay semantics, zero-FOUC theme initialization, motion preference safeguards, onboarding clarity, and visual token/iconography standards for Hub-hosted HTML workstations (`internal/service/chat.html` and `internal/service/admin.html`).

## Requirements

### Requirement: WCAG AA Contrast and Complete Theme Tokenization

Both hosted workstations SHALL achieve at least 4.5:1 text contrast on all primary buttons, badges, pills, and selected autocomplete options in both light and dark themes, while keeping all color literals scoped to `:root` / `:root[data-theme="dark"]` custom properties and preventing horizontal root overflow via `overflow-x: clip`.

#### Scenario: Operator uses /admin solid buttons in Dark Mode
- **WHEN** Dark Mode (`data-theme="dark"`) is active in `/admin`
- **THEN** primary and hover button surfaces maintain at least 4.5:1 foreground-to-background contrast via dedicated `--accent-btn-bg`, `--accent-btn-hover`, and `--accent-btn-fg` tokens without altering focus ring contrast.

#### Scenario: User switches themes in Chat Lounge without FOUC
- **WHEN** a user has stored a manual theme preference (`light` or `dark`) overriding the OS setting and reloads `/` or `/admin`
- **THEN** a CSP-nonced `<head>` initializer applies `data-theme` synchronously before body paint so no light/dark flash occurs.

### Requirement: Top-Layer Native Modal Dialogs

Interactive overlays in `chat.html` (`#skills-modal-backdrop` and `#resources-modal-backdrop`) SHALL use native modal `<dialog closedby="any">` opened via `showModal()`, providing top-layer stacking, background `inert` isolation, focus trapping, `overscroll-behavior: contain` scroll containment, `Esc` and backdrop dismissal (with pointer-coordinate fallback where `closedby` is unsupported), and automatic focus restoration.

#### Scenario: Keyboard user tabs inside Circle Skills or Circle Resources dialog
- **WHEN** the user opens either modal dialog and presses `Tab` repeatedly
- **THEN** focus cycles strictly inside the active dialog, background content remains `inert`, scrolling over the backdrop does not scroll the underlying timeline, and closing returns focus to the triggering control.

### Requirement: Mobile Sidebar Drawer Containment

On narrow viewports (`<= 820px`), opening the navigation sidebar drawer in `chat.html` SHALL mark background `.main-pane` `inert`, move focus into the drawer, restore focus upon `Escape`/backdrop dismissal, and auto-reset `inert` if the viewport widens past `820px`.

#### Scenario: Mobile user opens and dismisses sidebar roster drawer
- **WHEN** viewport width is `<= 820px` and the user opens the sidebar menu
- **THEN** background `.main-pane` is marked `inert` until the drawer is closed via close action, backdrop tap, `Escape`, or desktop resize.

### Requirement: Motion Sensitivity, Tactile Affordance, and Unified SVG Iconography

All CSS transitions SHALL honor `@media (prefers-reduced-motion: reduce)`, interactive buttons SHALL provide subtle active press feedback when motion is permitted, and action buttons in Skills/Resources editors (`chat.html`) and Skills formatting (`admin.html`) SHALL replace OS-rendered emojis with consistent inline linear SVG icons while preserving 3-language i18n label switching.

#### Scenario: User enables Reduced Motion and switches languages
- **WHEN** `prefers-reduced-motion: reduce` is enabled and the user switches between `繁體中文`, `English`, and `日本語`
- **THEN** slide/hover transitions disable cleanly, button SVG icons remain rendered alongside localized `<span>` labels without clipping or duplication.

### Requirement: Clear Pre-Connection Onboarding Stage and Accessible Autocomplete

While disconnected (`session == null`), `chat.html` SHALL avoid competing empty-state cards and disabled composer controls so `#connect-strip` presents a focused single entry step; upon connection, the timeline and composer unlock seamlessly. The `@` mention autocomplete (`#mention-popover`) SHALL expose combobox ARIA states (`aria-expanded`, `aria-controls`, `aria-activedescendant`) and stay within the viewport, while long timelines apply `content-visibility: auto` to `.msg-row`.

#### Scenario: Visitor opens `/` before connecting
- **WHEN** no browser session credential is active on load
- **THEN** `#connect-strip` displays clearly as the single onboarding surface without an inactive composer or conflicting empty-state box beneath it.

#### Scenario: Keyboard user navigates @ mention suggestions
- **WHEN** the user types `@` in the composer and presses `ArrowUp` / `ArrowDown`
- **THEN** `#composer-input` updates `aria-expanded="true"` and `aria-activedescendant` to the highlighted option ID and resets both cleanly when dismissed or applied.
