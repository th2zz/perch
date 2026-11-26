# Contributing to Perch

Contributions are welcome. Perch is organized into four components in one repository:

| Directory | Component | Stack |
|---|---|---|
| `app/` | Android mobile client | Kotlin, Jetpack Compose, Gradle (AGP 9), local xterm.js WebView |
| `bridge/` | Host-side daemon | Go |
| `plugin/` | `herdr` plugin manifest | TOML |
| `relay/` | Optional push notification relay (payload whitelist) | Go |
| `contracts/` | Cross-language JSON fixtures shared by Go and Android tests | JSON |

## Prerequisites

- Go 1.24+
- JDK 17 and Android SDK (for `app/`)
- Node.js 20+ (for `scripts/check-terminal-page.sh`)
- [`herdr`](https://github.com/herdrdev/herdr) 0.9+ (for live host integration tests)

## Running Tests Locally

```bash
# Go bridge & relay tests
(cd bridge && go test ./...)
(cd relay && go test ./...)

# Android unit tests & lint
(cd app && ./gradlew :app:testDebugUnitTest :app:lintRelease)

# Terminal WebView page contract & fit math tests
scripts/check-terminal-page.sh

# Full local verification suite (add --quick to skip release & cross-compile checks)
scripts/check-all.sh --quick
```

## Cross-Language Contracts

Both `bridge/` (Go) and `app/` (Kotlin) validate their encoders and decoders against the JSON files in `contracts/`:

- `contracts/api-shape.json`
- `contracts/bridge-events.json`
- `contracts/pairlink-cases.json`
- `contracts/wire-vocabulary.json`

Whenever you change wire types, event names, or pairing link encoding, update the shared contract and verify both Go and Android suites pass.

## Design Invariants

1. **Non-invasive host integration**: Perch reads and acts through `herdr` without modifying the user's `herdr` configuration.
2. **Preserve desktop pane geometry**: Viewing an agent session on a phone must not resize the desktop pane unless the user explicitly triggers a resize.
3. **Never guess unmappable prompts**: If a prompt dialog cannot be mapped unambiguously to key actions, refuse one-tap approval and let the user open the session screen.
