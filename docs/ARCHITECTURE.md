# Architecture

This document describes the wgpu-example-go project layout, the boundary between binaries and library code, the platform-specific build pipelines, and how the pieces come together at runtime. File paths are relative to the repository root.

## 1. Module layout

The repo is a single Go module declared at `go.mod`:

```
module wgpu-example-go

go 1.26.3

require (
    github.com/cogentcore/webgpu v0.23.0
    github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260406072232-3ac4aa2bb164
    github.com/go-gl/mathgl v1.2.0
)
```

Three direct dependencies. `cogentcore/webgpu` is the cgo binding over `wgpu-native`; `go-gl/glfw` provides the desktop windowing layer (also cgo); `go-gl/mathgl` is the column-major float32 linear-algebra library used throughout the renderer.

The directory layout follows the standard Go `cmd/<binary>/` convention for executables and uses sibling top-level packages for library code:

```
wgpu-example-go/
├── cmd/
│   ├── serve/main.go           # static file server for the wasm bundle
│   └── wgpu-example-go/        # the spinning-triangle demo
│       ├── main.go             # //go:build !js  - GLFW window + event loop
│       └── main_js.go          # //go:build js   - canvas + requestAnimationFrame loop
├── render/                     # package render - the reusable renderer
├── site/                       # Pages deploy source (index.html, main.wasm, wasm_exec.js)
├── docs/                       # architecture documentation (this directory)
├── .github/workflows/
│   ├── go.yml                  # CI: vet, fmt, lint, test, tidy, build (×2)
│   └── pages.yml               # builds wasm and pushes to gh-pages branch
├── go.mod
├── go.sum
└── justfile                    # recipe runner; see "Build flow" below
```

This matches Rust workspace conventions in spirit: `cmd/<name>/` plays the role of `apps/<name>/src/main.rs`, and `render/` plays the role of `crates/<name>/src/`. Rust requires the extra `src/` directory; Go does not.

## 2. Binary entrypoints (`cmd/`)

### 2.1 `cmd/wgpu-example-go/`

Two files share `package main` but compile mutually-exclusively via build tags:

- **`main.go` (`//go:build !js`).** Desktop entrypoint. Calls `runtime.LockOSThread()` from `init()` (required by GLFW; the window and event APIs must stay on the thread that called `glfw.Init`). `setupLogging()` reads `WGPU_LOG_LEVEL` (`OFF`/`ERROR`/`WARN`/`INFO`/`DEBUG`/`TRACE`) and forwards to `wgpu.SetLogLevel`. `main()` creates a 1280×720 GLFW window, hands its native surface descriptor to `wgpu.Instance.CreateSurface` via the `wgpuglfw` bridge, constructs a `render.Engine`, and enters the event loop. Loop body: poll events, compute `delta`, call `engine.RenderFrame(delta)`. Recoverable surface errors (matched with `errors.Is(err, render.ErrSurfaceLost)`) trigger `engine.Reconfigure()`; everything else `log.Fatal`s. Escape closes the window.

- **`main_js.go` (`//go:build js`).** Wasm entrypoint. Looks up `<canvas id="canvas">` in the DOM, creates an instance + surface bound to that canvas (`wgpu.SurfaceDescriptor{Canvas: canvas}`), constructs a `render.Engine`, and drives the frame loop with `requestAnimationFrame`. A `ResizeObserver` calls `engine.Resize(w, h)` when the canvas content box changes. The `js.FuncOf` callbacks (`resizeFunc` and `frame`) are deliberately kept alive for the page lifetime; `main` ends with `select {}` so the wasm module never returns.

Build tag selection: the Go toolchain implicitly applies `//go:build js` to any file ending `_js.go`. `main.go`'s explicit `//go:build !js` opts it out for wasm builds. The two files never coexist in one compilation.

### 2.2 `cmd/serve/main.go`

A 27-line `net/http.FileServer` wrapped to set `Content-Type: application/wasm` on `.wasm` requests. Defaults to serving `site/` on `:8080`. Used by `just serve` and indirectly by `just run-wasm`. No build tag — compiled as a normal Go binary.

## 3. Renderer library (`render/`)

`package render` is the entire renderer. It exposes a small public surface (`Engine`, `New`, `ErrSurfaceLost`, plus four methods on `Engine`) and keeps every other type unexported. See [`RENDERER.md`](./RENDERER.md) for the file-by-file walkthrough; this section just describes the boundary.

The package's external contract:

```go
package render

type Engine struct { /* opaque */ }

func New(instance *wgpu.Instance, surface *wgpu.Surface, width, height uint32) (*Engine, error)

func (e *Engine) RenderFrame(deltaTime float32) error
func (e *Engine) Resize(width, height uint32) error
func (e *Engine) Reconfigure()
func (e *Engine) Release()

var ErrSurfaceLost = errors.New("wgpu surface lost or outdated")
```

The binaries in `cmd/` consume only this. They do not import any other identifier from `render`. The rest of the package (gpu, scene, pipeline, uniformBinding, mathx, etc.) is implementation detail.

`Engine` is not safe for concurrent use. All methods must be called from the same goroutine — for the desktop binary, that's the main goroutine that `init()` locked to its OS thread; for wasm there are no concurrent goroutines.

## 4. Build flow

Every developer-facing operation is wrapped in the `justfile`. The recipes that matter:

| Recipe | What it runs |
|--------|--------------|
| `just run` | `go run ./cmd/wgpu-example-go` (desktop) |
| `just build` | `go build ./cmd/wgpu-example-go` |
| `just build-wasm` | `GOOS=js GOARCH=wasm go build -o site/main.wasm ./cmd/wgpu-example-go` |
| `just run-wasm` | `build-wasm` + `serve` |
| `just serve` | `go run ./cmd/serve` (serves `site/` on `:8080`) |
| `just check` | `go vet ./...` and `gofmt -l .` — fails on any unformatted file |
| `just test` | `go test ./...` |
| `just ci` | `check` + `test` |
| `just audit` | `check` + `tidy-check` + `outdated` + `test` |
| `just doc` | `go doc -all ./render` |

`set windows-shell := ["powershell.exe"]` at the top targets the developer's local environment (Windows). CI does not use the justfile — the workflow calls Go directly so the recipes don't need to be portable.

### 4.1 Desktop build path

`go build ./cmd/wgpu-example-go` produces `wgpu-example-go.exe` on Windows (binary name = directory name by Go convention). The build pulls in:

- `cmd/wgpu-example-go/main.go` (`!js`)
- `wgpu-example-go/render` (the whole package except `uniform_js.go`)
- `github.com/cogentcore/webgpu/wgpu` and `wgpuglfw` (cgo, bundled prebuilt `wgpu-native` static libs)
- `github.com/go-gl/glfw/v3.3/glfw` (cgo, links against system X11/GL/wayland headers on Linux)
- `github.com/go-gl/mathgl/mgl32`

The cgo dependencies mean a C toolchain must be on `PATH`. On Linux that means `libgl1-mesa-dev xorg-dev libxkbcommon-dev`; on Windows, a MinGW-w64 `gcc`; on macOS, Xcode command-line tools.

### 4.2 WebAssembly build path

`GOOS=js GOARCH=wasm go build -o site/main.wasm ./cmd/wgpu-example-go` produces `site/main.wasm`. The build pulls in:

- `cmd/wgpu-example-go/main_js.go` (`js`)
- `wgpu-example-go/render` (the whole package except `uniform_native.go`)
- `github.com/cogentcore/webgpu/wgpu` (pure-Go path that calls `navigator.gpu.*` through `syscall/js`; no cgo)
- `github.com/go-gl/mathgl/mgl32`

No cgo. No GLFW. No C toolchain required.

The runtime loader is Go's standard `wasm_exec.js` (copied from `$(go env GOROOT)/lib/wasm/wasm_exec.js` in Go 1.24+). It is committed at `site/wasm_exec.js` and refreshed by the Pages workflow on every deploy.

`site/index.html` is a minimal HTML shell that instantiates the wasm module via `WebAssembly.instantiateStreaming` and exposes `window.wasm` for the `cogentcore/webgpu` JS bindings (which read the linear-memory `ArrayBuffer` from `window.wasm.instance.exports.mem.buffer`).

## 5. Continuous integration (`.github/workflows/`)

Two workflows.

### 5.1 `go.yml`

Triggered on push to `main` and on every pull request. Seven parallel jobs on `ubuntu-latest`:

| Job | Step |
|-----|------|
| `Vet` | `go vet ./...` (needs xorg-dev for GLFW type-check) |
| `Fmt` | `gofmt -l .` and fail if non-empty |
| `Lint (staticcheck)` | `dominikh/staticcheck-action@v1` (needs xorg-dev) |
| `Test` | `go test ./...` (needs xorg-dev) |
| `Tidy` | `go mod tidy -diff` |
| `Build wasm` | `GOOS=js GOARCH=wasm go build -o main.wasm ./cmd/wgpu-example-go` |
| `Build desktop` | `go build ./cmd/wgpu-example-go` (needs xorg-dev) |

All jobs that compile any package containing `go-gl/glfw` install `libgl1-mesa-dev xorg-dev libxkbcommon-dev` first — the GLFW headers transitively `#include <X11/Xlib.h>`, which fails type-check without those system packages. `Fmt`, `Tidy`, and `Build wasm` skip the apt install: `gofmt` and `go mod` don't compile cgo, and `GOOS=js` excludes cgo entirely.

### 5.2 `pages.yml`

Triggered on push to `main`. One job:

1. Checkout
2. `actions/setup-go@v5` with `go-version: stable`
3. `GOOS=js GOARCH=wasm go build -o site/main.wasm ./cmd/wgpu-example-go`
4. `cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" site/wasm_exec.js`
5. `JamesIves/github-pages-deploy-action@v4` with `folder: site` and `single-commit: true`

The deploy action pushes `site/` to the `gh-pages` branch (replacing the previous tip — `single-commit: true` means no growing history on that branch). GitHub Pages is configured to serve from `gh-pages` / root.

The live URL is `https://matthewjberger.github.io/wgpu-example-go/` (subject to any custom domain configured on the user's GitHub Pages settings).

## 6. Runtime composition

A successful frame on either platform follows the same sequence:

1. The platform shim (`main.go` or `main_js.go`) computes `delta` (seconds since last frame).
2. `engine.RenderFrame(delta)` advances `scene.model` by `HomogRotate3DY(degToRad(30) * delta)` and builds `mvp = projection · view · model`. `projection` comes from `perspectiveZO` (mgl32 RH perspective composed with a depth-range remap). `view` comes from `mgl32.LookAtV`. The resulting `mgl32.Mat4` is written to the uniform buffer via the platform-specific `uniformBinding.update`.
3. The platform shim acquires the next surface texture (`gpu.surface.GetCurrentTexture()`), creates a view, encodes a single render pass (color attachment = swapchain view, depth attachment = `Depth32Float` with clear-then-store), binds the pipeline + uniform group + vertex/index buffers, issues one indexed draw, ends the pass, submits the command buffer, calls `Present()`.
4. If `GetCurrentTexture` returns one of the recoverable surface errors (`Surface timed out`, `is outdated`, `was lost`, `Outdated`), `RenderFrame` wraps it in `ErrSurfaceLost` and returns. The platform shim matches with `errors.Is` and calls `engine.Reconfigure()` so the next frame rebuilds the swap chain.

The depth attachment matters because the pipeline uses `CompareFunctionLess` with clear value `1.0`, so each frame starts with depth = 1.0 (far plane) everywhere and only writes fragments with smaller depth values. With one triangle this is irrelevant; with future geometry, it's correct.

## 7. Conventions

- **Matrices.** `mgl32.Mat4` is `[16]float32`, column-major. WGSL `mat4x4<f32>` is column-major. The uniform layout in Go (`type uniformBuffer struct { mvp mgl32.Mat4 }`) matches the WGSL declaration (`struct Uniform { mvp: mat4x4<f32>; }`) byte-for-byte; no transpose, no padding.
- **Clip space.** `mgl32` produces OpenGL clip space (x, y ∈ [-1, 1] with y up; z ∈ [-1, 1]). wgpu expects D3D-style clip space (x, y ∈ [-1, 1] with y up; z ∈ [0, 1]). The x/y conventions match. The z range is bridged on the CPU by `perspectiveZO`, which composes `mgl32.Perspective` with the `ndcZTo01` constant matrix. The bridge happens once per frame on the host side; the vertex shader is `out.position = ubo.mvp * vert.position` and stays platform-agnostic.
- **Handedness.** Both `mgl32.LookAtV` and `mgl32.Perspective` are right-handed (OpenGL convention). The `perspectiveZO` wrapper does not change handedness — it only remaps depth. The pipeline's `FrontFaceCW` is effectively ignored because `CullMode = CullModeNone`.
- **Byte uploads.** `(*uniformBuffer).bytes()` uses `unsafe.Slice((*byte)(unsafe.Pointer(u)), unsafe.Sizeof(*u))` to expose the struct as `[]byte` without copying. This is the only direct `unsafe.Pointer` cast in the codebase; `unsafe.Sizeof` is used elsewhere as a compile-time constant.
- **Error wrapping.** All errors crossing package boundaries use `fmt.Errorf("...: %w", err)`. The surface-lost sentinel (`ErrSurfaceLost`) is the only typed error users match against.
- **Build tags.** `_js.go` suffix auto-applies a `//go:build js` constraint. The default-platform file pairs are unsuffixed (`engine.go`, `main.go`, `uniform.go`) and carry `//go:build !js` where they need it. The `_native` suffix on `uniform_native.go` is descriptive — not a Go build-tag convention — but its `//go:build !js` is explicit.
- **Comments.** Doc comments on every exported identifier in `render` (`Engine`, `New`, `ErrSurfaceLost`, each method). Inline comments only where the *why* is non-obvious (the unsafe byte slice, the recoverable surface message list, the deliberately leaked `js.FuncOf` callbacks).

## 8. Where things deliberately diverge from the Rust reference

The Go port matches the Rust `wgpu-example` in scope (single rotating RGB triangle) but takes a few different choices because of language/ecosystem constraints:

- **Math library.** Rust uses `nalgebra-glm`, which ships LH zero-to-one perspective and look-at variants directly. Go's `go-gl/mathgl` ships only OpenGL RH conventions. We use `mgl32.Perspective` + a constant depth-range remap in `perspectiveZO`; the view matrix is `mgl32.LookAtV` (RH) instead of the Rust LH lookat. The visible orientation of the triangle differs (Rust's LH lookat produces an X-flip the Go RH lookat does not).
- **GUI library.** Rust's reference uses `egui` for a debug overlay. The Go port has no GUI library — the demo is rendering-only.
- **Platforms.** Rust's reference supports desktop, WASM (WebGPU + WebGL2 fallback), Android, OpenXR, and Steam Deck. The Go port supports desktop and WASM (WebGPU only). Neither WebGL nor mobile is in scope.
- **Build tooling.** Rust uses `cargo` + `trunk`. Go uses `go build` + `just` recipes + a small `cmd/serve` static file server for local wasm testing.

See [`RENDERER.md`](./RENDERER.md) for the renderer-specific implementation walkthrough.
