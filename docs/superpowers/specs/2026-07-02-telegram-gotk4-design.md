# Telegram client on gotk4 — design

Date: 2026-07-02
Status: approved design, revised after 4-agent review (gotd feasibility, gotk4
capability, scope/completeness, security)

## Goal

Prove the gotk4 bindings work end-to-end by building a real, usable Telegram
client with them. Secondary: close the one concrete gap the app exposes in
`pkg/gota` (async image widget).

## Decisions

- **Full real client** via [gotd/td](https://github.com/gotd/td) (pure-Go
  MTProto). User supplies their own `api_id`/`api_hash` through an in-app
  setup screen.
- **V1 features:** login (phone → code → optional 2FA), dialog list, message
  history (latest ~50), send/receive text realtime, inline photo rendering,
  mark-as-read on open, logout. Other media: placeholders (`[file]`,
  `[sticker]`, `[voice]`).
- **Lives in this repo** at `examples/telegram/`, its own Go module,
  `replace github.com/diamondburned/gotk4/pkg => ../../pkg`, `go 1.26`.
  Dependencies: `gotd/td`, `gotd/contrib` (floodwait middleware).
- **Architecture A: direct gotd + thin event glue.** No backend abstraction
  layer, no tdlib.

## Repo review outcome

No real bugs found. Root module tests pass (only `pkgconfig.TestGIRDirs`
fails, environmental on macOS/Homebrew — heterogeneous keg prefixes; Linux CI
green). `pkg` builds; `core`/`gota` tests pass. Related but **separate**
work items (not gating, not part of the app plan):

1. `TestGIRDirs`: skip with reason when no common install prefix resolves
   (brew environment) — own small PR.
2. `pkg/gota`: async image widget — identified as the one real gota gap.
   v1 keeps the loader app-local (`internal/ui/photo.go`) to avoid gating
   the app on a cross-module PR; upstreamed to gota AFTER v1 ships, with
   the app as its consuming evidence.

## App layout

```
examples/telegram/
  go.mod                 # replace → ../../pkg; deps gotd/td, gotd/contrib
  main.go                # gtk.Application wiring
  internal/config/       # api_id/api_hash JSON, 0600, in os.UserConfigDir()/gotk4-telegram/ (dir 0700)
  internal/tg/           # gotd wrapper: lifecycle, auth, peers, dialogs,
                         # history, send, updates, photo download+cache
  internal/ui/           # setup.go, login.go, chat.go, photo.go; view models ui.Dialog, ui.Message
```

## Client lifecycle (load-bearing constraints)

gotd RPC is valid **only inside the `client.Run(ctx, f)` callback**; `Run`
blocks until the connection closes. Therefore:

- A background goroutine calls `client.Run`; the callback runs the auth flow,
  checks `Auth().Status`, starts the updates manager, signals the UI
  "ready" (via `IdleAdd`), then blocks until shutdown/logout.
- UI-triggered RPCs run in their own goroutines and are only issued between
  "ready" and shutdown (client is concurrency-safe while running).
- Reconnect: transport-level reconnects are automatic while `Run` is live;
  if `Run` exits with an error, re-enter it with capped exponential backoff
  (1s → 60s max, until ctx cancel), UI shows a connecting banner.
- FLOOD_WAIT: `gotd/contrib/middleware/floodwait.NewSimpleWaiter()` via
  `telegram.Options.Middlewares`.

## Auth

- `auth.NewFlow(uiAuthenticator, auth.SendCodeOptions{}).Run(ctx, client.Auth())`
  executed inside the Run callback.
- `uiAuthenticator` implements the full `auth.UserAuthenticator` interface:
  `Phone`, `Code`, `Password` block on a channel each while the UI (told via
  `IdleAdd`) shows the matching login page; `AcceptTermsOfService` returns
  nil; `SignUp` returns an error (sign-up out of scope).
- The authenticator is an interface consumed by `internal/tg`, so unit tests
  drive it with a scripted fake (no network).
- Errors: invalid `api_id`/`api_hash` (`API_ID_INVALID`) → error message →
  back to setup page. Wrong code/password → retry on same page. Error UI
  never echoes the entered code/password.
- Session: gotd `session.FileStorage` in the config dir. **The session file
  is the highest-value secret** (full account access): file 0600, dir 0700.
  A session invalidated mid-use (`AUTH_KEY_UNREGISTERED`) → delete session
  file → login pages.
- Logout: menu action → `auth.Logout` best-effort → delete session file →
  login pages.
- Secrets hygiene: 2FA password entry masked (gota `EntryWidget.PasswordMode`),
  never persisted, never logged; gotd logger stays off/info in release.

## Peers, dialogs, updates

- Supported peers v1: users, basic groups, supergroups, channels (read).
  Sending: users + groups + supergroups. Posting to broadcast channels: out
  of scope.
- `internal/tg` keeps an in-memory peer store (ID → `InputPeer` with
  `access_hash`), populated from `query.GetDialogs` results and updates.
  Not persisted in v1 → full dialog fetch on each launch (accepted).
- Dialogs: `telegram/query` dialogs iterator, stop at 50.
- Realtime: a bare `tgapi.UpdateDispatcher` wired as
  `telegram.Options.UpdateHandler` (v1 does not persist update state, so
  `updates.Manager`'s only marginal benefit would be mid-session gap
  recovery — deferred to v2 with persistence; adversarial review verdict).
- New-message events → Go channel → `coreglib.IdleAdd` → append to open chat
  + refresh sidebar preview/unread.

## Messages

- History: `query` GetHistory, latest 50, rendered oldest-top. Loading older
  messages (pagination): **out of scope v1**.
- Send: `message.NewSender` to the known `InputPeer` (no username
  re-resolution). No optimistic rows (cut after adversarial review — the
  machinery was dead weight): the sent message is appended once by feeding
  the sendMessage RPC response updates (including `UpdateShortSentMessage`)
  through the same ingestion path as live updates; a per-chat
  seen-message-ID set dedupes double delivery. Send failure shows an error
  banner (no auto-retry).
- Mark-as-read: `messages.readHistory` on opening a chat.
- Rendering: GTK labels with markup **disabled** (plain text) for message
  bodies and sidebar previews — no Pango injection from message content.

## UI

- One `gtk.Stack`: setup page → login pages (phone/code/password) → main.
- Main: sidebar `gtk.ListView` backed by `gioutil.NewListModel[ui.Dialog]`
  (generic model from `pkg/core/gioutil`, no manual GObject boxing) |
  message pane: `ListView` backed by `gioutil.NewListModel[ui.Message]` |
  composer: single-line `gtk.Entry`, Enter-to-send via activate (gota
  `EntryWidget.OnEnter`). Multiline composer: out of scope.
- Reuse `pkg/gota` where it fits (RunApp, boxes, Entry, Label, Scrolled);
  chat panes use raw ListView (gota Table is a ColumnView — wrong shape).
- Errors/status: top-of-window `gtk.Revealer` banner (InfoBar is deprecated
  since GTK 4.10). Connecting/offline state shown in the same banner.
- Empty states: placeholder label for zero dialogs / empty chat.

## Photos

- From `tg.Photo.Sizes` (union type): consider only variants carrying real
  dimensions (`PhotoSize`, `PhotoSizeProgressive`); pick the largest with
  width ≤ 800px, else the smallest available; pass that size's `Type` letter
  as `ThumbSize` in `InputPhotoFileLocation`.
- Download via `telegram/downloader` `.ToPath()` into
  `~/.cache/gotk4-telegram/` (dir 0700). Cache filename is locally generated:
  hex photo ID + size letter — never server-supplied strings; final path
  asserted inside the cache root. Download size capped (10 MB).
- `FILE_REFERENCE_EXPIRED` → refetch the message once, retry download once.
- Async: download goroutine → `IdleAdd` → `gtk.Picture` (app-local async
  loader; upstreamed to gota post-v1); placeholder while loading.

## Threading

Iron rule: gotd/network calls only in goroutines; all UI mutation through
`coreglib.IdleAdd` (verified: `pkg/core/glib` — gbox uintptr-id model, safe
from any goroutine). This is the bindings surface the app exists to prove.

## Testing and evidence

- Unit: config roundtrip; auth flow with scripted fake authenticator;
  message/dialog view-model mapping incl. seen-ID dedup + optimistic
  replacement; photo size selection; cache path containment.
- Manual E2E: real login on dev machine; screenshots = PR evidence.
- CI: compile `examples/telegram` in the Linux/Nix job. macOS/Windows jobs
  untouched.

## Phases

1. Scaffold + config + lifecycle skeleton + setup/login UI → real login
   succeeds, session persists.
2. Peer store + dialogs + history + realtime send/receive text + read-marking
   + logout.
3. gota async image widget (pkg PR) + photo display.
4. Polish (banner states, failed-send, empty states) + CI build hook +
   README/screenshot. (`TestGIRDirs` skip: separate PR, any time.)

## Out of scope (v1)

History pagination, multiline composer, posting to broadcast channels,
persisted peer/updates state, stickers/voice/video rendering, edit/delete,
search, profiles, notifications, multi-account, OS keychain, sign-up flow,
packaging.
