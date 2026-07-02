# Telegram client on gotk4 — design

Date: 2026-07-02
Status: approved (brainstorming session)

## Goal

Prove the gotk4 bindings work end-to-end by building a real, usable Telegram
client with them. Secondary goal: close the few remaining rough edges the app
work exposes in this repo (gota gaps, environmental test failure on macOS).

## Decisions (from brainstorming)

- **Full real client**, not an offline demo. Connects to Telegram via
  [gotd/td](https://github.com/gotd/td) (pure-Go MTProto). The user supplies
  their own `api_id`/`api_hash` through an in-app setup screen.
- **V1 feature set: text core + photo display.** Login (phone → code →
  optional 2FA password), dialog list, message history, send/receive text in
  realtime, inline photo rendering. Other media shows placeholders
  (`[file]`, `[sticker]`, `[voice]`).
- **Lives in this repo** at `examples/telegram/` as its own Go module with a
  `replace` directive pointing at `../../pkg`, so the app always builds
  against the working tree and CI can compile it.
- **Architecture A: direct gotd + thin event glue.** No `ChatBackend`
  abstraction layer (YAGNI for a single backend); no tdlib (heavy C
  dependency, defeats the "prove gotk4" purpose).

## Repo review outcome

The review found **no real bugs**:

- Root module (generator) tests pass. The only failure, `pkgconfig.TestGIRDirs`,
  is environmental on macOS/Homebrew: brew installs each package into its own
  Cellar keg, so a single install prefix cannot be resolved. Documented in
  CLAUDE.md; CI (Linux/Nix) is green.
- `pkg` module: `gtk/v4` and `gota` build; `core` and `gota` tests pass.
- No open PRs or issues; recent PRs (#10–#12) already added a generator test
  harness, benchmarks, and race-scoped CI.

Completion items bundled into this effort (kept deliberately small, no broad
refactors per CONTRIBUTING):

1. Make `TestGIRDirs` skip with an explanatory reason when the environment
   cannot resolve a common install prefix (heterogeneous brew kegs), instead
   of failing red on a non-bug.
2. Any `pkg/gota` gaps the app exposes are fixed in `gota` with tests, rather
   than worked around inside the app.

## App layout

```
examples/telegram/
  go.mod                 # module …/examples/telegram; replace → ../../pkg
  main.go                # gtk.Application wiring, IdleAdd marshaling helper
  internal/config/       # api_id/api_hash JSON at os.UserConfigDir()/gotk4-telegram/, 0600
  internal/tg/           # gotd wrapper: client lifecycle, auth flow, dialogs,
                         # history, send, updates dispatch, photo download+cache
  internal/ui/           # setup.go (creds form), login.go (phone/code/password),
                         # chat.go (sidebar + messages + composer), photo.go (async picture)
```

## Auth and storage

- gotd `auth.Flow` with a custom `UserAuthenticator` driven by the UI: the
  gotd goroutine blocks on a channel per step (phone, code, 2FA password) and
  each login page sends the entered value into that channel.
- Session persistence: gotd `session.FileStorage` in the config directory, so
  a restart with a valid session skips login entirely.
- Credentials are stored as a plaintext JSON file with 0600 permissions.
  OS-keychain storage is explicitly out of scope for v1.

## Data flow

- **Startup:** missing config → setup page; config present → connect →
  needs auth? → login pages → main view. Views live in one `gtk.Stack`.
- **Main view:** sidebar `ListView` with the top ~50 dialogs (title, last
  message preview, unread count) | message pane rendering ~50 history
  messages (`messages.getHistory`) | text composer.
- **Send:** `messages.sendMessage` with optimistic local append.
- **Receive:** gotd updates handler → Go channel → `coreglib.IdleAdd` →
  append to the open chat and refresh the sidebar preview.
- **Photos:** photo media is downloaded asynchronously (largest size class
  whose width ≤ 800px, falling back to the smallest available) into
  `~/.cache/gotk4-telegram/`, then rendered via
  `gtk.Picture` on the main loop. A placeholder shows while downloading.

## Threading and error handling

- Iron rule: gotd/network calls run only in goroutines; all UI mutation goes
  through `coreglib.IdleAdd`. This exercises the bindings' hardest surface —
  the gbox/closure registry and the "no Go pointers across cgo" model.
- Errors surface in an infobar; FLOOD_WAIT handled by gotd's ratelimit
  middleware; simple reconnect with backoff.

## Testing and evidence

- Unit tests: config load/save roundtrip, tg→UI message model mapping, photo
  cache path logic (pure logic, no network).
- E2E is manual: real login on the dev machine; screenshots serve as PR
  evidence per the user's global PR rules.
- CI: compile `examples/telegram` in the Linux/Nix job (GTK available there).
  macOS/Windows CI jobs untouched.

## Phases

1. Scaffold + config + setup/login UI → a real login succeeds.
2. Dialog list + history + realtime send/receive text.
3. Photo display.
4. Polish (errors, reconnect) + CI build hook + README/screenshot +
   `TestGIRDirs` graceful skip.

## Out of scope (v1)

Stickers/voice/video rendering, message editing/deleting, search, profiles,
notifications, multi-account, OS keychain, packaging/installers.
