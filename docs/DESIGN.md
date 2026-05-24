# Software Design Document

This document is the *why* behind wgpu-example-go. Goals and non-goals, the requirements the code is built to satisfy, the design decisions that shaped it, and the alternatives that were considered and dropped. It is the design-rationale companion to the two descriptive docs: [`ARCHITECTURE.md`](./ARCHITECTURE.md) is the outside-in project view (directories, binaries, build flow, CI) and [`RENDERER.md`](./RENDERER.md) is the file-by-file walk through `package render`. Where this document would repeat them, it links instead.

## 1. Purpose and scope

The project renders a spinning RGB triangle. One window, one triangle, two platforms: native desktop (GLFW) and the browser (WebGPU via WASM). It exists as a minimal, correct, idiomatic-Go reference for driving `wgpu` from Go across both targets, and as the Go member of a family of ports (Rust, C, Zig, Odin, Vulkan, OpenGL) listed in the [`README`](../README.md).

In scope:

- A reusable `render` package that owns the GPU surface, swap chain, depth target, scene, and frame submission.
- Two thin platform entrypoints that own the window or canvas and the frame loop.
- A build and CI flow that keeps both targets compiling and formatted.

Out of scope, deliberately: GUI, text, textures, multiple meshes, scene graphs, input beyond Escape-to-quit, audio, XR, mobile, asset loading. The triangle is the product. Anything that does not serve drawing it correctly on both platforms is not here.

## 2. Goals and non-goals

### Goals

1. **One renderer, two platforms.** The drawing logic is written once and compiles for both desktop and WASM. Platform difference is isolated to the smallest possible surface.
2. **Correctness over features.** Coordinate conventions, color space, and byte layouts are handled correctly even though a single triangle would survive several of those bugs unnoticed. The code is meant to be copied as a starting point, so latent bugs would propagate.
3. **Idiomatic Go.** Standard `cmd/` layout, build tags rather than runtime platform checks, errors wrapped with `%w`, exported identifiers documented, no framework ceremony.
4. **Self-contained static web build.** After a build, `site/` is hostable as-is with no server-side code.
5. **Readable as a teaching artifact.** Small public API, comments only where the *why* is non-obvious, the GPU object lifecycle explicit and visible.

### Non-goals

- **Performance.** The per-frame WASM buffer recreation (section 5.6) is wasteful by design. No batching, instancing, or pipeline caching beyond what wgpu does itself. Correct and clear beats fast for a demo.
- **Robust production lifecycle.** On a construction error, GPU objects leak (section 5.10). `Release` does not zero pointers, so double-release is unsafe. These are documented, accepted demo-grade tradeoffs.
- **API stability.** The `render` package is a reference, not a published library. Its surface can change.
- **Headless or offscreen rendering.** Both targets render to a presented surface.

## 3. Requirements

### Functional

- F1. Open a 1280x720 desktop window titled for the demo, or bind to a `<canvas id="canvas">` in the browser.
- F2. Render a triangle with red, green, and blue vertices, color-interpolated across the face, over a fixed dark-blue background.
- F3. Rotate the triangle about the world Y axis at a constant 30 degrees per second, independent of frame rate.
- F4. Track surface resizes and rebuild the swap chain and depth target to match.
- F5. Recover from a lost or outdated surface without crashing.
- F6. Quit on Escape or window close (desktop). Run for the page lifetime (browser).

### Non-functional

- N1. The renderer compiles unchanged into both the `!js` and `js` builds except for one file pair.
- N2. No cgo on the WASM path. No C toolchain required to build for the web.
- N3. CPU-to-GPU matrix transfer happens with no transpose and no per-frame allocation on desktop.
- N4. Color output is linear (not silently gamma-shifted by the surface format).
- N5. Depth testing is wired correctly so adding a second mesh needs no depth changes.
- N6. `go vet`, `gofmt`, `staticcheck`, and `go test` all pass in CI for both targets.

## 4. System overview

Three layers, top to bottom:

```
cmd/wgpu-example-go/  platform shim   window/canvas, surface creation, frame loop, delta time
        │  (owns the loop, calls down)
        ▼
render/               Engine          surface, swap chain, depth, scene, frame submission
        │  (owns GPU state, knows no platform)
        ▼
cogentcore/webgpu     wgpu binding    cgo over wgpu-native (desktop) / syscall/js (browser)
```

The shim depends on `render`; `render` never depends on the shim. The only thing crossing back up is the `ErrSurfaceLost` sentinel, which the shim matches with `errors.Is` to decide whether to reconfigure and retry. See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the directory and binary layout and [`RENDERER.md`](./RENDERER.md) for the internals of `render`.

## 5. Design decisions

Each decision below is recorded as context, the decision taken, why, the alternatives rejected, and the consequences accepted.

### 5.1 Library/binary split

**Context.** Two platforms must share drawing logic but differ in windowing and loop.

**Decision.** Put all GPU and scene logic in a platform-agnostic `render` package exporting six identifiers (`Engine`, `New`, `RenderFrame`, `Resize`, `Reconfigure`, `Release`, `ErrSurfaceLost`). Keep windowing, surface creation, and the frame loop in `cmd/wgpu-example-go/`.

**Why.** The platform-specific code is small and the GPU code is large. Inverting that (a platform package that imports drawing helpers) would scatter GPU state across the boundary. A narrow, documented `Engine` API is the contract both shims code against.

**Alternatives rejected.** A single `main` package with everything inline (no reuse, no test seam). An interface-based abstraction layer over windowing (overkill for two callers that share no behavior).

**Consequences.** `render` has no `main`, no window type, no awareness of GLFW or canvas. It is the part worth copying into another project.

### 5.2 Build tags over runtime platform checks

**Context.** Desktop and WASM diverge in three places: surface creation, the frame loop, and one GPU upload path.

**Decision.** Use Go build tags. `main.go` carries `//go:build !js`; `main_js.go` is implicitly `js` by filename. In `render`, only `uniform_native.go` (`!js`) and `uniform_js.go` (`js`) differ; everything else compiles into both.

**Why.** Build tags give each target exactly the code it needs and nothing else. No `syscall/js` symbols in the desktop binary, no GLFW or cgo in the WASM binary. A runtime `if platform == ...` would require both code paths to type-check and link on both targets, which is impossible: `syscall/js` does not exist off-WASM and GLFW does not exist on it.

**Alternatives rejected.** Runtime branching (cannot link). Two separate modules (duplicated `render`, drift risk).

**Consequences.** The divergence is auditable: it is exactly the two `uniform_*.go` files. The architecture doc states this invariant and CI builds both targets to keep it true.

### 5.3 cogentcore/webgpu as the binding

**Context.** Go needs to reach WebGPU on both wgpu-native (desktop) and `navigator.gpu` (browser).

**Decision.** Depend on `github.com/cogentcore/webgpu`, which presents one Go API backed by cgo over wgpu-native on desktop and a `syscall/js` wrapper over `navigator.gpu` in the browser.

**Why.** A single binding that already abstracts both backends is exactly the seam the library/binary split needs. The desktop side bundles prebuilt wgpu-native static libs, so a contributor needs only a C toolchain, not a separate wgpu-native build.

**Consequences.** The binding's leaks become ours to manage: opaque string errors for surface state (section 5.7) and the detached-ArrayBuffer hazard on WASM (section 5.6). Both are contained behind the renderer's API.

### 5.4 mgl32 for math, with a CPU clip-space remap

**Context.** The renderer needs 4x4 matrix math. `mgl32` is the standard Go choice, but it targets OpenGL conventions while wgpu uses D3D/Vulkan/Metal conventions for the depth range.

**Decision.** Use `mgl32` (column-major `[16]float32`, right-handed, OpenGL clip space) and bridge the one convention gap on the CPU. `perspectiveZO` composes `mgl32.Perspective` with a constant matrix `ndcZTo01` that remaps post-divide depth from OpenGL's `[-1, 1]` to wgpu's `[0, 1]`.

**Why.** x and y conventions already agree between `mgl32` and wgpu; only z differs. Fixing z once per frame on the host with a constant matrix keeps the vertex shader a plain `mvp * position` and avoids any platform-specific WGSL. Column-major `mgl32.Mat4` matches column-major WGSL `mat4x4<f32>` byte-for-byte, so the upload needs no transpose.

**Alternatives rejected.** Patching the depth range in the shader (scatters the assumption into WGSL, must be kept in sync). Hand-rolling a `_ZO` perspective matrix (a derivation to get wrong; `perspectiveZO` instead mirrors `glm::perspectiveRH_ZO`, a named composition of library primitives). Switching math libraries (no Go option fixes the z range natively).

**Consequences.** Handedness is unchanged: `LookAtV` and `Perspective` are both right-handed and `perspectiveZO` only touches depth. The triangle winds CCW from the camera, which would be back-facing under `FrontFaceCW`, so the pipeline uses `CullModeNone`. Documented in [`RENDERER.md`](./RENDERER.md#projectiongo-clip-space-bridge).

### 5.5 Non-sRGB surface format

**Context.** `surface.GetCapabilities` returns a list of supported formats, some sRGB, some linear.

**Decision.** Prefer the first non-sRGB format; fall back to `caps.Formats[0]` if none is non-sRGB. `isSrgb` checks against `RGBA8UnormSrgb` and `BGRA8UnormSrgb`.

**Why.** The fragment shader emits final colors directly. An sRGB surface would apply an electro-optical transfer function on write, brightening and shifting the gradient. A linear format means the byte written is the brightness shown, which is what the per-vertex color interpolation expects.

**Alternatives rejected.** Taking `caps.Formats[0]` unconditionally (often sRGB; wrong colors). Doing gamma correction in the shader (extra work to undo a transform we can simply not opt into).

**Consequences.** Satisfies requirement N4. If a platform only offers sRGB formats, the fallback accepts the shift rather than failing.

### 5.6 Per-frame buffer recreation on WASM

**Context.** On the `js` path, `cogentcore/webgpu`'s `Queue.WriteBuffer` builds a typed-array *view* over WASM linear memory via `jsx.BytesToJS`. When Go's runtime grows the heap, the underlying `ArrayBuffer` detaches and the next view construction throws `Cannot perform Construct on a detached ArrayBuffer`.

**Decision.** On WASM only, recreate the uniform buffer and bind group every frame with `CreateBufferInit`, which copies through `js.CopyBytesToJS` (a real copy that survives heap growth). The bind group layout is stable, so the pipeline is never touched. Desktop keeps the cheap `queue.WriteBuffer` path.

**Why.** It is the one path that survives heap growth. The alternative spellings all eventually hit the detached buffer.

**Alternatives rejected.** Using `WriteBuffer` on WASM (the bug being worked around). Pre-growing or pinning WASM memory (not exposed by the Go runtime). A single shared code path for both targets (would force the wasteful path onto desktop too).

**Consequences.** WASM allocates a buffer and bind group per frame; desktop does not. This is the single largest desktop/WASM behavior difference and the clearest justification for the build-tag split. On failure the previous buffer and bind group stay in place and the next frame retries. Documented in [`RENDERER.md`](./RENDERER.md#wasm-upload-uniform_jsgo).

### 5.7 Surface-lost recovery via sentinel plus string match

**Context.** wgpu-native does not expose typed errors for surface state. Every surface error arrives as an opaque string.

**Decision.** Keep a list `recoverableSurfaceMessages` of known substrings (`Surface timed out`, `Surface is outdated`, `Surface was lost`, `Outdated`). `wrapSurfaceErr` wraps a match in the exported `ErrSurfaceLost` sentinel with `%w`; anything else passes through unwrapped. Callers use `errors.Is(err, ErrSurfaceLost)` and never match strings themselves.

**Why.** Substring matching is the only handle the binding gives. Quarantining it in one file behind a sentinel means a future wgpu-native version that renames a message is a one-line edit in `errors.go`, not a change in caller code.

**Alternatives rejected.** Matching strings in the platform shims (spreads the brittleness across binaries). Treating all `GetCurrentTexture` errors as recoverable (would mask genuine faults in a retry loop).

**Consequences.** The recovery contract is: `RenderFrame` returns `ErrSurfaceLost`, the shim calls `Reconfigure`, the next frame rebuilds the swap chain. `Reconfigure` is separate from `Resize` because the swap chain can also die from monitor changes, suspend/resume, or driver resets, with no size change.

### 5.8 Zero-copy uniform upload via unsafe.Slice

**Context.** The MVP matrix lives in an `mgl32.Mat4` in Go memory and must be handed to wgpu as bytes every frame.

**Decision.** `(*uniformBuffer).bytes()` returns `unsafe.Slice((*byte)(unsafe.Pointer(u)), unsafe.Sizeof(*u))`: a byte view over the struct with no copy. This is the only direct `unsafe.Pointer` cast in the codebase.

**Why.** The struct is exactly one column-major `mat4x4` (64 bytes), byte-identical to the WGSL declaration, no padding. Copying would be pure overhead on the per-frame hot path (desktop). `unsafe.Sizeof` is also used as a compile-time constant for the vertex stride and bind-group size so no byte count is ever hardcoded.

**Alternatives rejected.** `binary.Write` or manual `float32` extraction (allocates and copies every frame). A hardcoded `64`/`32` size (drifts if the struct changes).

**Consequences.** The returned slice aliases the receiver and must not outlive the `update` call. The byte-layout agreement between Go and WGSL is load-bearing and is stated in both this doc and [`RENDERER.md`](./RENDERER.md#uniformbuffer).

### 5.9 Depth buffer present despite a single triangle

**Context.** One triangle never occludes anything, so a depth attachment changes no pixels.

**Decision.** Allocate a `Depth32Float` target, attach it every frame cleared to 1.0, and configure the pipeline with `DepthWriteEnabled` and `CompareFunctionLess`.

**Why.** The demo is a starting point. Wiring depth correctly now means a second mesh gets occlusion for free with no pipeline or attachment changes (requirement N5). Leaving it out would push that work onto whoever extends the demo and invite a subtly wrong reintroduction.

**Consequences.** A per-frame depth clear that does no visible work, and a depth texture rebuilt on every resize.

### 5.10 Demo-grade error and lifecycle handling

**Context.** Production GPU code tracks partial construction and guards against misuse. That machinery is large.

**Decision.** `New` returns the first error unwrapped and leaks any GPU objects already created on that path. `Release` walks owned objects in dependency order (scene, depth view, queue, device, adapter, surface) with nil guards but does not zero pointers. `Engine` is single-goroutine and not safe for concurrent use.

**Why.** The wgpu-native messages are already informative, the process exits on a construction failure anyway, and the single-goroutine model holds on both targets (desktop locks the main goroutine to its OS thread for GLFW; WASM has no other goroutines). The cost of full teardown-on-error and double-release safety is not worth it for a demo.

**Consequences.** Accepted and documented limitations: leak on construction error, unsafe double-release, no concurrency. Listed again in section 12.

## 6. Data design

### Vertex

`vertex` is two `[4]float32` fields, position then color, 32 bytes, described to the pipeline as two `Float32x4` attributes at offsets 0 and 16 with a stride of `unsafe.Sizeof(vertex{})`. The fourth position component is the homogeneous `w = 1`, which lets a single 4x4 matrix express translation and perspective. The three vertices carry red, green, and blue; the gradient across the face is hardware interpolation of those three, not anything the fragment shader computes.

### Index

`indices = [3]uint32{0, 1, 2}`. Redundant for three vertices, kept to exercise the `IndexFormatUint32` indexed-draw path that a real mesh would use.

### Uniform

`uniformBuffer` is one `mgl32.Mat4` (the MVP), 64 bytes, column-major, matching WGSL `struct Uniform { mvp: mat4x4<f32> }` byte-for-byte with no padding. The Go-to-WGSL byte agreement (column-major on both sides, no transpose) is the load-bearing data contract of the renderer.

## 7. Interface design

### Public API

The six identifiers in section 4 are the entire contract between the shims and the renderer. Full signatures and per-method behavior are in [`RENDERER.md`](./RENDERER.md#the-public-surface). The shape: construct with `New`, drive with `RenderFrame(delta)` once per frame, feed `Resize` from a resize callback, call `Reconfigure` after `ErrSurfaceLost`, and `Release` on shutdown.

### Platform shim contract

A shim must: create a `wgpu.Instance` and a `wgpu.Surface` for its window or canvas, call `render.New`, then each frame compute `delta` in seconds and call `RenderFrame`. It owns the loop, the resize source, and the quit condition. It must call all `Engine` methods from one goroutine. Desktop satisfies this with `runtime.LockOSThread` in `init` (GLFW requires its calls on the thread that called `Init`); WASM satisfies it trivially.

## 8. Coordinate systems and math

The full chain, model space to pixels:

```
model ──model──▶ world ──view──▶ camera ──projection──▶ clip ──(GPU ÷w)──▶ NDC ──(GPU viewport)──▶ pixels
```

- **Model.** Accumulated rotation about world Y. `s.model = s.model · HomogRotate3DY(30deg · delta)`. Multiplying by `delta` makes the spin frame-rate independent (requirement F3).
- **View.** `LookAtV(eye {0,0,3}, target {0,0,0}, up {0,1,0})`, right-handed.
- **Projection.** `perspectiveZO(80deg, aspect, 0.1, 1000)`: `mgl32.Perspective` composed with the `ndcZTo01` depth remap (section 5.4).
- **Compose.** `mvp = projection · view · model`. With column vectors on the right (`ubo.mvp * vert.position`), the rightmost matrix applies first, so model acts before view before projection.
- **GPU fixed-function.** Perspective divide (the foreshortening) and viewport transform are done by hardware on the shader's `@builtin(position)` output, not by us.

Convention summary, because mixing these up is the classic source of silent rendering bugs:

| Property | mgl32 (source) | wgpu (target) | Bridge |
|----------|----------------|---------------|--------|
| Matrix storage | column-major | column-major | none needed |
| x, y NDC range | [-1, 1], y up | [-1, 1], y up | none needed |
| z NDC range | [-1, 1] | [0, 1] | `ndcZTo01` on CPU |
| Handedness | right-handed | (set by view) | unchanged |

## 9. Concurrency and threading

`Engine` is single-goroutine. Every method must be called from the goroutine that constructed it. `RenderFrame`, `Resize`, and `Reconfigure` may interleave but never run concurrently; in practice a resize callback runs synchronously on the main thread between `RenderFrame` calls. Desktop enforces single-threaded GLFW access by locking the main goroutine to its OS thread in `init`. WASM is single-threaded by nature. There is no locking inside `render` because there is nothing to lock against.

## 10. Error handling and recovery

Two categories:

- **Fatal.** Construction errors from `New` and unrecognized `RenderFrame` errors. The desktop shim `log.Fatal`s; the WASM shim logs to the console and stops. No recovery is attempted because these indicate a broken environment, not a transient state.
- **Recoverable.** Surface-lost, detected by `wrapSurfaceErr` and surfaced as `ErrSurfaceLost` (section 5.7). The shim reconfigures and retries on the next frame.

Errors crossing the package boundary are wrapped with `fmt.Errorf("...: %w", err)` so callers can `errors.Unwrap` to the original wgpu message. `ErrSurfaceLost` is the only typed error callers match.

## 11. Resource lifecycle

GPU objects are not managed by Go's garbage collector and are freed explicitly. Long-lived objects (surface, adapter, device, queue, pipeline, buffers, bind group, depth view) are owned by `Engine` and torn down by `Release` in dependency order. Per-frame transients (surface texture view, command encoder, render pass, command buffer) are released with `defer` or an explicit call within `RenderFrame`. The surface texture itself has no `Release`; wgpu-native owns it. On WASM the uniform buffer and bind group are additionally recreated and released every frame (section 5.6). Resize releases the old depth view before building the new one.

## 12. Constraints, assumptions, and known limitations

**Constraints.**
- Desktop requires a C toolchain on `PATH` (cgo) and a GPU supporting Vulkan, D3D12, or Metal.
- WASM requires a browser with WebGPU (recent Chromium, Firefox 141+, Safari 26+).
- The web build depends on Go's `wasm_exec.js` from `GOROOT` (Go 1.24+ path), and on `index.html` exposing `window.wasm` for the binding to read linear memory.

**Assumptions.**
- The surface offers at least one usable format, present mode, and alpha mode.
- The recoverable-surface-error strings remain stable enough across wgpu-native versions; if not, `errors.go` is the single edit point.
- `mgl32.Mat4` stays column-major `[16]float32` (the unsafe byte view depends on it).

**Known limitations (accepted for a demo).**
- GPU objects leak if `New` fails partway.
- `Release` does not zero pointers; double-release is unsafe.
- `Engine` is not concurrency-safe.
- The accumulated model matrix drifts over time because each frame multiplies the previous matrix rather than rebuilding from a scalar angle. Production code would store the angle.
- WASM allocates a uniform buffer and bind group per frame.

## 13. Build, tooling, and CI

All developer commands go through the `justfile`; see [`ARCHITECTURE.md`](./ARCHITECTURE.md#builds) for the recipe table and the desktop/WASM build inputs. CI (`go.yml`) fans out into vet, fmt, staticcheck, test, tidy, and both builds, installing X11 dev packages only for the jobs that compile GLFW. `pages.yml` builds `site/main.wasm`, refreshes `wasm_exec.js`, and single-commits `site/` to the `gh-pages` branch. The build invariant that protects this design is N1: the renderer compiles into both targets unchanged except for the `uniform_*.go` pair, and CI builds both targets on every push and PR to keep that true.

## 14. Testing strategy

The demo's correctness is primarily visual: the triangle spins, the gradient is smooth, colors are not washed out (linear surface), and resizing does not distort or crash. CI guarantees both targets compile and pass `go vet`, `gofmt`, `staticcheck`, and `go test ./...`. There are no GPU unit tests; a headless GPU in CI is out of scope, and the logic worth testing (the matrix composition and the clip-space remap) is small enough to verify by reading and by observing output. Future pure-function tests could cover `perspectiveZO` and the MVP composition without a device.

## 15. Future work

The design leaves clean extension points, in rough order of effort:

- **More geometry.** Depth is already wired (section 5.9); a second mesh needs only more vertex data and draw calls.
- **Textures.** Add a texture and sampler binding to `uniformBinding`'s layout, or a second bind group.
- **Camera input.** The view matrix is already isolated in `scene.update`; driving it from input is local.
- **Stable model rotation.** Replace the accumulated matrix with a stored scalar angle to remove drift.
- **Pure-function tests.** Cover the projection remap and MVP order without a GPU.

Anything larger (GUI, XR, mobile, asset pipeline) belongs in a different project; this one stays the minimal cross-platform triangle.
