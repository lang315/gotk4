# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

gotk4 is a **GTK4 (and GTK3) bindings generator for Go**. The repository holds both the generator and its output:

- **Root module** `github.com/diamondburned/gotk4` (`gir/`) — the GIR parser and the code generator. AGPLv3.
- **`pkg/` module** `github.com/diamondburned/gotk4/pkg` — the generated cgo bindings plus a hand-written `core/`. MPLv2 (`pkg/cairo` is MIT).

These are **two separate Go modules**. The generator does not import the bindings; the bindings are produced by running the generator.

## Commands

Canonical dev environment is Nix (pins exact GTK/GLib versions so generation is reproducible): `nix-shell` or `nix develop`. Docker alternative in CONTRIBUTING.md. Bindings won't build without the GTK/GLib pkg-config packages present.

```sh
# Regenerate ALL of pkg/ from the system .gir files (run from repo root):
go generate                       # = go run ./gir/cmd/gir-generate -o ./pkg/
GIR_VERBOSE=1 go generate         # explains why each type/function is skipped

# Test — TWO modules, run both (CI does):
go test ./...                     # generator (root module)
cd pkg && go test ./...           # bindings

# Single test:
go test ./gir/girgen/generators/ -run TestEnumMemberAliasDedup -v
go test ./gir/girgen/ -run TestGTK4Coverage -v   # GTK4 completeness guard (needs GTK .gir; skips otherwise)

# Lint — goimports, NOT just gofmt (preserves manual import groupings):
goimports -w .
```

CI (`.github/workflows/qa.yml`) runs `go generate`, `goimports -w .`, and both test suites, each followed by a **git-dirty check**: regenerated output and formatting must produce no diff. Generation must be **idempotent** — running `go generate` repeatedly yields identical output. A dirty tree fails CI. `.github/workflows/build.yml` additionally builds the bindings on Linux (Nix, full tree) / macOS / Windows (the portable GTK4 subset).

`TestGTK4Coverage` (`gir/girgen/coverage_test.go`) is a **completeness guard**: it parses the system Gtk/Gdk/Gsk-4.0 `.gir` and asserts every public, bindable type is generated in `pkg/` or listed as an intentional skip/rename — so a GTK bump can't silently drop API. Update its `intentionalSkips`/`renames` maps when changing `gendata.go` `Filters`/`TypeRenamer`.

### Building/verifying bindings without Nix (e.g. macOS)

`brew install gtk4 graphene gobject-introspection gdk-pixbuf`, then before any `go build`/`go test` in `pkg/`:
```sh
export PKG_CONFIG_PATH="$(brew --prefix)/lib/pkgconfig:$(brew --prefix)/share/pkgconfig:$PKG_CONFIG_PATH"
```
Caveat: a regen on macOS produces a **macOS-flavored tree** (GdkMacos instead of GdkWayland/GdkX11, different Gio) and currently won't build `gio/v2` (pulls `GOsxAppInfo`). Don't commit a macOS regen. To verify a generator change, regen to a scratch `-o` dir and build only the affected package over a copy of the committed tree. `gir/pkgconfig`'s `TestGIRDirs` fails without GTK on PATH — that's environmental, not a code bug.

## Generator architecture (the big picture)

Flow: `.gir` XML → parse → resolve types → emit Go+cgo per GIR element.

- **`gir/`** — parses GIR XML into Go structs (`types.go`), plus `pkgconfig/` (locates `.gir` dirs via pkg-config).
- **`gir/cmd/gir-generate/gendata/gendata.go`** — the **generation config and the place to change what gets generated**: the list of pkg-config packages/namespaces, `Filters` (skip types/functions), `Postprocessors` (inject extra hand-authored generated files, e.g. glib aliases), and preprocessors that rewrite GIR before generation. Missing/broken bindings are usually fixed here (un-filter a type, add a postprocessor), not by editing `pkg/`.
- **`gir/girgen/`** — the engine:
  - `generator.go`, `namespace.go` — drive generation per namespace.
  - `generators/` — one emitter per GIR element: `enum.go`, `bitfield.go`, `class-interface.go`, `record.go`, `union.go`, `function.go`, `callback.go`, `callable/`, `iface/`. `FormatEnumMember`/`strcases.SnakeToGo` map C identifiers → Go names (and **collapse underscores**, which can collide distinct members onto one Go name).
  - `types/` — `resolve.go` (GIR type → Go/C type), `filter.go`, and **`typeconv/`** (`go-c.go`, `c-go.go`) which generate the marshaling/ownership/free logic in both directions (callback scopes, transfer-ownership, GError, etc.). This is where the hard memory-semantics decisions live.
  - `gotmpl/` Go text/templates, `pen/` output writer, `cmt/` doc-comment formatting, `logger/` (debug skips).

## Generated vs hand-written code

- **`pkg/{gtk,gdk,gio,glib,gobject,...}/`** are **100% generated** — any manual edit is wiped by the next `go generate`. To add/fix a binding, change the generator or `gendata.go`.
- **`pkg/core/`** is **hand-written** and safe to edit directly: `glib` (GValue, signal marshaling, idle/timeout, closures), `gbox` (Go-callback registry), `closure`, `gerror`, `gextras`, `intern`, `slab`, `gioutil`, `girepository`. The generated `pkg/glib/v2` etc. alias/wrap `core`.

## Memory & cgo model

- Uses `go4.org/unsafe/assume-no-moving-gc`. **Never pass a Go pointer to C across a cgo boundary** — Go callbacks/closures are stored in the `gbox` registry and only an opaque `uintptr` id is passed to C (see `core/glib` idle/timeout/signal code). Breaking this reintroduces "invalid pointer found on stack" crashes.
- Each generated package carries its own `#cgo pkg-config:` directive listing only the libraries it actually links.

## Branches & versioning

Output is **GTK-version-specific**; the pinned GTK version determines the generated API. The pin lives in `flake.nix` as the `nixpkgs-gotk4` input rev (currently a nixos-unstable rev with GTK **4.22.4**); bump that rev to change the target GTK version. Per README: branch `4`/`main` ⇒ GTK 4.22, `4.16` ⇒ GTK 4.16. Regenerating against a newer GTK changes output drastically; Nixpkgs is never downgraded (would break existing user code). `main` is this fork's default branch and tracks `4`.

Both modules target **Go 1.26** (the `go` directive in each `go.mod`). In Nix, `flake.nix` sources the Go toolchain from the **`nixpkgs-gotk4` pin** (`go_1_26`), *not* the rolling `nixpkgs` (nixos-unstable) input — that input's lock can lag and lack the required Go. CI's macOS `build.yml` pins `setup-go` to the same minor. Keep all three aligned when bumping Go.

## Conventions (from CONTRIBUTING.md)

- Always format with `goimports`, not just `go fmt`.
- Avoid project-wide refactors — they are expensive to review and discouraged.
- Don't add `.gitignore` entries for editor scrap; use a global gitignore instead.
