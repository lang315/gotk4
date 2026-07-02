# gotk4 Telegram client

## Purpose

A real Telegram client — MTProto via [gotd/td](https://github.com/gotd/td),
UI via gotk4/GTK4 — built as proof that the gotk4 bindings work end-to-end in
a real application, not just in isolated demos. It exercises the generated
GTK4/GLib/GIO bindings plus the hand-written `pkg/core` runtime (idle
dispatch from non-UI goroutines, `gioutil` list models, `gota` fluent
widgets) under a genuine network client with login, realtime updates, and
media.

## Prerequisites

GTK4 must be available, either via Nix or Homebrew:

- Nix: `nix-shell` (or `nix develop`) from the repo root, matching the
  gotk4/pkg dev environment.
- Homebrew (macOS):

  ```sh
  brew install gtk4 graphene gobject-introspection gdk-pixbuf
  export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
  ```

You'll also need a Telegram `api_id`/`api_hash` pair, obtained free at
[my.telegram.org](https://my.telegram.org) → API development tools.

## Run

```sh
cd examples/telegram
go run .
```

On first run the app shows a setup page for `api_id`/`api_hash`, then walks
through the login prompts (phone, code, and two-factor password if enabled).
On subsequent runs the saved session is reused and the app connects straight
to the main chat view.

## Storage

| Path | Contents | Permissions |
|---|---|---|
| `$XDG_CONFIG_HOME/gotk4-telegram/` (or platform equivalent, via `os.UserConfigDir()`) | config dir | 0700 |
| `.../gotk4-telegram/config.json` | `api_id` / `api_hash` | 0600 |
| `.../gotk4-telegram/session.json` | MTProto session — **full account secret**, equivalent to a login credential | 0600 |
| `$XDG_CACHE_HOME/gotk4-telegram/` (or platform equivalent, via `os.UserCacheDir()`) | downloaded photo cache | 0700 |

`session.json` grants full access to the Telegram account it belongs to —
treat it like a password. Logging out (the "Logout" button in the chat view)
best-effort revokes the session server-side and deletes the local file
before returning to the login page.

## v1 scope

In scope: text and photo messaging (core), realtime updates (new messages
via the update dispatcher), login including phone/code/two-factor password,
mark-as-read on opening a chat, and logout.

Out of scope for v1 (see
`docs/superpowers/specs/2026-07-02-telegram-gotk4-design.md`): history
pagination (only the latest 50 messages load), multiline composer, posting
to broadcast channels, stickers/voice/video rendering, edit/delete, search,
profiles, notifications, multi-account, update-gap recovery, persisted
peer/updates state, OS keychain, sign-up flow, packaging.

## Screenshots

Manual E2E screenshots (macOS/Quartz) go here — not yet committed.

![login (macOS/Quartz)](docs/login.png)
![chat (macOS/Quartz)](docs/chat.png)
![photos (macOS/Quartz)](docs/photos.png)

TODO: replace the placeholders above with real screenshots before merging;
Linux rendering is covered only by the CI build, not by screenshots.

## v2: gap recovery + pagination

v1 does not persist update state, so a bare `tgapi.UpdateDispatcher` is used
and there is no recovery of updates missed while offline — only messages
seen while the app is running are shown. History pagination (loading
messages older than the initial 50) is also deferred. Both are planned for
v2, once update/peer state persistence lands.
