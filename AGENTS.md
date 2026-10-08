# Crab.GitSync

- Desktop app: Go + Wails 3 (v3.0.0-beta.28), React + TypeScript + Vite.
- `gitservice.go` owns task state, cancellation and the Wails service API.
- `internal/gitengine` discovers and inspects repositories and invokes the system Git.
- `frontend/src` owns the UI. `frontend/bindings` is generated; do not edit it manually.
- Regenerate bindings after changing exported service APIs: `wails3 generate bindings -ts -i`.
- Build frontend before Go validation because `main.go` embeds `frontend/dist`.
- Required checks: `cd frontend; npm ci; npm run build`, then from project root `go test ./...`, `go vet ./...` and `wails3 build`.
- Sync is fetch only. Do not add pull, merge, reset, checkout or push without a user request.
- Git tests must create disposable repositories in `t.TempDir()`, never change user repositories or use live credentials.
