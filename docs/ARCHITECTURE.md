# Architecture

This is the wgpu-example-go project from the outside in. What the directories hold, where the binary/library line sits, how the desktop and wasm builds differ, and what a frame looks like when the whole thing runs.

The whole thing is a spinning RGB triangle on desktop (GLFW) and in the browser (canvas + WebGPU). No GUI, no XR, no mobile. If you want the file-by-file walkthrough of the renderer package instead, see [`RENDERER.md`](./RENDERER.md).

## The module

One Go module, declared in `go.mod`:

```
module wgpu-example-go

go 1.26.3

require (
    github.com/cogentcore/webgpu v0.23.0
    github.com/go-gl/glfw/v3.3/glfw v0.0.0-20260406072232-3ac4aa2bb164
    github.com/go-gl/mathgl v1.2.0
)
```

Three direct dependencies. [`cogentcore/webgpu`](https://github.com/cogentcore/webgpu) is the cgo binding over `wgpu-native` on desktop and a `syscall/js` wrapper around `navigator.gpu` in the browser. [`go-gl/glfw`](https://github.com/go-gl/glfw) is the desktop window and event layer (also cgo). [`go-gl/mathgl`](https://github.com/go-gl/mathgl) is the column-major float32 linear-algebra library the renderer uses for every matrix.

Directory layout follows the standard Go convention of `cmd/<binary>/` for executables and sibling top-level packages for library code:

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
└── justfile
```

Executables live under `cmd/<name>/`; library code lives in sibling top-level packages.

## Binaries

There are two: the demo itself and a tiny static file server for testing the wasm bundle locally.

### `cmd/wgpu-example-go/`

Two files share `package main` and compile mutually-exclusively via build tags.

`main.go` (`//go:build !js`) is the desktop entrypoint. It calls `runtime.LockOSThread()` from `init()` because GLFW will deadlock the moment you call its window or event APIs from a thread other than the one that called `glfw.Init`. `setupLogging()` reads `WGPU_LOG_LEVEL` (`OFF`/`ERROR`/`WARN`/`INFO`/`DEBUG`/`TRACE`) and forwards it to `wgpu.SetLogLevel`. `main()` opens a 1280×720 GLFW window, hands its native surface descriptor to `wgpu.Instance.CreateSurface` through the `wgpuglfw` bridge, constructs a `render.Engine`, and enters the event loop. Each iteration polls events, computes `delta`, and calls `engine.RenderFrame(delta)`. Recoverable surface errors (matched with `errors.Is(err, render.ErrSurfaceLost)`) trigger `engine.Reconfigure()`; anything else is fatal. Escape closes the window.

`main_js.go` (`//go:build js`) is the wasm entrypoint. It looks up `<canvas id="canvas">` in the DOM, creates an instance plus surface bound to that canvas (`wgpu.SurfaceDescriptor{Canvas: canvas}`), constructs a `render.Engine`, and drives the frame loop with `requestAnimationFrame`. A `ResizeObserver` calls `engine.Resize(w, h)` whenever the canvas content box changes size. The `js.FuncOf` callbacks (`resizeFunc` and `frame`) are deliberately kept alive for the page lifetime, and `main` ends with `select {}` so the wasm module never returns. Releasing those callbacks would unhook the loop.

The Go toolchain implicitly applies `//go:build js` to any file whose name ends in `_js.go`. The explicit `//go:build !js` on `main.go` opts it out for wasm builds. The two files never coexist in one compilation.

### `cmd/serve/`

Twenty-seven lines of `net/http.FileServer` wrapped to set `Content-Type: application/wasm` on `.wasm` requests. Defaults to serving `site/` on `:8080`. Used by `just serve` and indirectly by `just run-wasm`. No build tag; it compiles as a normal Go binary.

## The renderer package

`package render` is the whole renderer. The public surface is small:

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

That is everything the binaries in `cmd/` import. The rest of the package (`gpu`, `scene`, `pipeline`, `uniformBinding`, the projection helper) is implementation detail and unexported. `RENDERER.md` walks through each file.

`Engine` is not safe for concurrent use. Every method has to be called from the goroutine that constructed it. On desktop that goroutine is the one `init()` locked to an OS thread for GLFW. On wasm there are no other goroutines to worry about.

## Builds

Every developer-facing command goes through the `justfile`. The recipes that matter:

| Recipe | What it runs |
|--------|--------------|
| `just run` | `go run ./cmd/wgpu-example-go` (desktop) |
| `just build` | `go build ./cmd/wgpu-example-go` |
| `just build-wasm` | `GOOS=js GOARCH=wasm go build -o site/main.wasm ./cmd/wgpu-example-go` |
| `just run-wasm` | `build-wasm` + `serve` |
| `just serve` | `go run ./cmd/serve` (serves `site/` on `:8080`) |
| `just check` | `go vet ./...` and `gofmt -l .`; fails on any unformatted file |
| `just test` | `go test ./...` |
| `just ci` | `check` + `test` |
| `just audit` | `check` + `tidy-check` + `outdated` + `test` |
| `just doc` | `go doc -all ./render` |

`set windows-shell := ["powershell.exe"]` at the top is for the developer's local machine. CI does not touch the justfile; the workflow shells out to `go` directly, so the recipes do not have to be portable.

### Desktop

`go build ./cmd/wgpu-example-go` produces `wgpu-example-go.exe` on Windows (Go names the binary after the directory). The build pulls in:

- `cmd/wgpu-example-go/main.go` (`!js`)
- `wgpu-example-go/render` (everything except `uniform_js.go`)
- `github.com/cogentcore/webgpu/wgpu` and `wgpuglfw` (cgo, with prebuilt `wgpu-native` static libs bundled)
- `github.com/go-gl/glfw/v3.3/glfw` (cgo, links system X11/GL/wayland headers on Linux)
- `github.com/go-gl/mathgl/mgl32`

cgo means a C toolchain on `PATH`. On Linux that is `libgl1-mesa-dev xorg-dev libxkbcommon-dev`. On Windows it is MinGW-w64 `gcc`. On macOS it is the Xcode command-line tools.

### WebAssembly

`GOOS=js GOARCH=wasm go build -o site/main.wasm ./cmd/wgpu-example-go` produces `site/main.wasm`. The build pulls in:

- `cmd/wgpu-example-go/main_js.go` (`js`)
- `wgpu-example-go/render` (everything except `uniform_native.go`)
- `github.com/cogentcore/webgpu/wgpu` (pure-Go path that calls `navigator.gpu.*` through `syscall/js`; no cgo)
- `github.com/go-gl/mathgl/mgl32`

No cgo. No GLFW. No C toolchain.

The runtime loader is Go's standard `wasm_exec.js`, copied from `$(go env GOROOT)/lib/wasm/wasm_exec.js` (Go 1.24+). The committed copy lives at `site/wasm_exec.js` and the Pages workflow refreshes it on every deploy. `site/index.html` is a minimal HTML shell that instantiates the wasm module via `WebAssembly.instantiateStreaming` and exposes `window.wasm` for the `cogentcore/webgpu` JS bindings (they read the linear-memory `ArrayBuffer` from `window.wasm.instance.exports.mem.buffer`).

## CI and deploys

Two workflows under `.github/workflows/`.

`go.yml` triggers on push to `main` and on every pull request, and fans out into seven parallel jobs on `ubuntu-latest`:

| Job | Step |
|-----|------|
| `Vet` | `go vet ./...` (needs xorg-dev for GLFW type-check) |
| `Fmt` | `gofmt -l .` and fail if non-empty |
| `Lint (staticcheck)` | `dominikh/staticcheck-action@v1` (needs xorg-dev) |
| `Test` | `go test ./...` (needs xorg-dev) |
| `Tidy` | `go mod tidy -diff` |
| `Build wasm` | `GOOS=js GOARCH=wasm go build -o main.wasm ./cmd/wgpu-example-go` |
| `Build desktop` | `go build ./cmd/wgpu-example-go` (needs xorg-dev) |

Every job that compiles any package containing `go-gl/glfw` installs `libgl1-mesa-dev xorg-dev libxkbcommon-dev` first. The GLFW headers transitively `#include <X11/Xlib.h>` and the type-checker can't get past that without the system packages. `Fmt`, `Tidy`, and `Build wasm` skip the apt install. `gofmt` and `go mod` never compile cgo, and `GOOS=js` excludes cgo entirely.

`pages.yml` triggers on push to `main` only and does one thing: checkout, set up Go, build `site/main.wasm` for `GOOS=js`, copy a fresh `wasm_exec.js` out of `$(go env GOROOT)`, then push `site/` to the `gh-pages` branch via `JamesIves/github-pages-deploy-action@v4` with `single-commit: true` (so that branch never accumulates history). Pages serves from `gh-pages` at root. The live URL is `https://matthewjberger.github.io/wgpu-example-go/`.

## A frame

A frame on either platform follows the same sequence.

1. The platform shim (`main.go` or `main_js.go`) computes `delta` (seconds since the last frame).
2. `engine.RenderFrame(delta)` advances `scene.model` by `HomogRotate3DY(degToRad(30) * delta)` and composes `mvp = projection · view · model`. `projection` comes from `perspectiveZO`, which is `mgl32.Perspective` followed by a constant depth-range remap. `view` is `mgl32.LookAtV`. The resulting `mgl32.Mat4` is written to the uniform buffer via the platform-specific `uniformBinding.update`.
3. The platform shim acquires the next surface texture (`gpu.surface.GetCurrentTexture()`), creates a view, encodes a single render pass (color = swapchain view, depth = `Depth32Float` cleared to 1.0 and stored), binds the pipeline plus uniform group plus vertex and index buffers, issues one indexed draw, ends the pass, submits the command buffer, and calls `Present()`.
4. If `GetCurrentTexture` returns one of the recoverable surface errors (`Surface timed out`, `is outdated`, `was lost`, `Outdated`), `RenderFrame` wraps it in `ErrSurfaceLost` and returns. The platform shim matches with `errors.Is` and calls `engine.Reconfigure()` so the next frame rebuilds the swap chain.

The depth attachment is doing nothing useful for one triangle. The pipeline uses `CompareFunctionLess` with clear value `1.0`, so each frame starts with depth = 1.0 (far plane) everywhere and only writes fragments with smaller depth values. It is wired up correctly so the moment a second piece of geometry shows up, occlusion works.

## Conventions

A few things worth knowing if you read the code:

- **Matrices.** `mgl32.Mat4` is `[16]float32` column-major. WGSL `mat4x4<f32>` is column-major. The Go uniform layout (`type uniformBuffer struct { mvp mgl32.Mat4 }`) matches the WGSL declaration (`struct Uniform { mvp: mat4x4<f32>; }`) byte-for-byte. No transpose, no padding.
- **Clip space.** `mgl32` produces OpenGL clip space (`x, y ∈ [-1, 1]` with y up, `z ∈ [-1, 1]`). wgpu wants D3D-style clip space (`x, y ∈ [-1, 1]` with y up, `z ∈ [0, 1]`). The x/y agree. The z range is bridged on the CPU once per frame by `perspectiveZO`, which composes `mgl32.Perspective` with the `ndcZTo01` constant matrix. The vertex shader stays `out.position = ubo.mvp * vert.position`. No platform-specific WGSL.
- **Handedness.** `mgl32.LookAtV` and `mgl32.Perspective` are both right-handed (OpenGL). `perspectiveZO` only remaps depth and does not change handedness. The pipeline's `FrontFaceCW` is effectively a no-op because `CullMode = CullModeNone`.
- **Byte uploads.** `(*uniformBuffer).bytes()` uses `unsafe.Slice((*byte)(unsafe.Pointer(u)), unsafe.Sizeof(*u))` to expose the struct as `[]byte` without copying. That is the only direct `unsafe.Pointer` cast in the codebase. `unsafe.Sizeof` is used a few other places as a compile-time constant.
- **Error wrapping.** Anything crossing a package boundary uses `fmt.Errorf("...: %w", err)`. The surface-lost sentinel is the only typed error callers ever match against.
- **Build tags.** `_js.go` auto-applies `//go:build js`. The default-platform companions (`engine.go`, `main.go`, `uniform.go`) carry `//go:build !js` where they need it. The `_native` suffix on `uniform_native.go` is descriptive (it has no Go-toolchain meaning), but the `//go:build !js` on the file is explicit.
- **Comments.** Doc comments on every exported identifier in `render` (`Engine`, `New`, `ErrSurfaceLost`, each method). Inline comments only where the *why* is non-obvious: the unsafe byte slice, the recoverable surface message list, the deliberately leaked `js.FuncOf` callbacks.

For the renderer's own internals, keep reading at [`RENDERER.md`](./RENDERER.md).
