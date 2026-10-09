# Crab.GitSync

- Desktop app: Go + Wails 3 (v3.0.0-beta.28), React + TypeScript + Vite.
- `gitservice.go` owns task state, cancellation and the Wails service API.
- `internal/gitengine` discovers and inspects repositories and invokes the system Git.
- `updateservice.go` wraps the Wails self-updater; `internal/updatefeed` authenticates GitHub Releases and requires SHA-256 checksums before staging an executable.
- Release source: public repository `CrabGo/Crab.GitSync`. Public updates use the stable Releases redirect and asset download URLs, without credentials or API quotas. SHA-256 verification is required.
- `Version` in `version.go` is overridden with `-X main.Version` by release builds. Windows release asset: `crab-gitsync-windows-amd64.exe`, plus `SHA256SUMS`.
- Build and validate a release with `./build/release.ps1 -Version X.Y.Z`. Pushing a stable `vX.Y.Z` tag triggers the GitHub release workflow.
- Update tests use HTTP test servers and temporary staging files. Never call Restart against the developer's application during tests.
- `frontend/src` owns the UI. `frontend/bindings` is generated; do not edit it manually.
- Regenerate bindings after changing exported service APIs: `wails3 generate bindings -ts -i`.
- Build frontend before Go validation because `main.go` embeds `frontend/dist`.
- Required checks: `cd frontend; npm ci; npm run build`, then from project root `go test ./...`, `go vet ./...` and `wails3 build`.
- Batch sync and the context-menu fetch action remain fetch only. Explicit context-menu merge, discard and fetch-then-merge actions are user-authorized features; require confirmation, revalidate branch/worktree state, preserve new/untracked files during discard, and keep conflicts visible. Never mutate developer/user repositories during validation.
- Git tests must create disposable repositories in `t.TempDir()`, never change user repositories or use live credentials.
