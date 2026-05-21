# Renderer

This is a file-by-file walk through `package render`. What each file owns, why it exists, and how the pieces stack up into the `Engine` type that `cmd/wgpu-example-go/` drives. All paths are relative to `render/`.

For the project-level view (binaries, build flow, CI), see [`ARCHITECTURE.md`](./ARCHITECTURE.md).

## The public surface

The package exports exactly six identifiers. Everything else is package-private.

```go
type Engine struct{ /* opaque */ }

func New(instance *wgpu.Instance, surface *wgpu.Surface, width, height uint32) (*Engine, error)

func (e *Engine) RenderFrame(deltaTime float32) error
func (e *Engine) Resize(width, height uint32) error
func (e *Engine) Reconfigure()
func (e *Engine) Release()

var ErrSurfaceLost = errors.New("wgpu surface lost or outdated")
```

The binaries in `cmd/` import only these. Nothing else.

## What sits in which file

| File | Build tag | Role |
|------|-----------|------|
| `engine.go` | none | `Engine` type, `New`, `RenderFrame`, `Resize`, `Reconfigure`, `Release` |
| `errors.go` | none | `ErrSurfaceLost`, `recoverableSurfaceMessages`, `isSurfaceLost`, `wrapSurfaceErr` |
| `gpu.go` | none | Internal `gpu` type: surface, adapter, device, queue, surface config, depth texture |
| `pipeline.go` | none | `createPipeline`, shader source embedded via `//go:embed shader.wgsl` |
| `projection.go` | none | `ndcZTo01` constant matrix, `perspectiveZO` wrapper |
| `scene.go` | none | `vertex`, `vertices`, `indices`, `scene`, `newScene`, `update`, `render`, `release` |
| `shader.wgsl` | none | Vertex + fragment WGSL source |
| `uniform.go` | none | `uniformBuffer`, `bytes`, `uniformBinding`, `newUniformBinding`, `release` |
| `uniform_native.go` | `//go:build !js` | Desktop `(*uniformBinding).update` via `queue.WriteBuffer` |
| `uniform_js.go` | `//go:build js` | Wasm `(*uniformBinding).update` via per-frame `CreateBufferInit` |

The split between `uniform_native.go` and `uniform_js.go` is the only place in the renderer where the desktop and wasm code paths diverge. Everything else compiles into both builds unchanged.

## `engine.go`: the type the binaries see

`Engine` owns three things:

```go
type Engine struct {
    gpu              *gpu
    depthTextureView *wgpu.TextureView
    scene            *scene
}
```

`gpu` is the surface, adapter, device, queue, surface configuration, and surface format. `depthTextureView` is a 32-bit-float depth attachment sized to match the surface, re-created on resize. `scene` is the vertex and index buffers, the uniform binding, and the render pipeline.

### `New`

`New(instance, surface, width, height)` takes ownership of an already-created instance and surface and:

1. Calls `newGpu(instance, surface, width, height)` to acquire an adapter, request a device, pick a non-sRGB surface format, configure the surface, and stash the queue.
2. Builds the depth-texture view via `gpu.createDepthTexture(width, height)`.
3. Builds the `scene` via `newScene(gpu.device, gpu.surfaceFormat)` (vertex/index buffers, uniform binding plus bind-group layout, render pipeline).
4. Returns the `Engine` or the first non-nil error from any of the three steps.

The failure modes are returned unwrapped. The wgpu-native error messages are already informative, and there is nothing meaningful to add. On the error path GPU objects do leak. For a one-shot demo this is acceptable. The caller is responsible for `Release` on success and for not using a half-constructed engine on failure.

### `RenderFrame`

`RenderFrame(deltaTime)` is one frame:

1. `e.scene.update(...)` rebuilds the model matrix (rotate by `30° * deltaTime` around Y), composes `mvp = projection · view · model`, and uploads to the uniform binding. Returns whatever the platform-specific `uniformBinding.update` returns. Always `nil` on desktop, possibly a wrapped wgpu error on wasm if the per-frame `CreateBufferInit` fails.
2. `e.gpu.surface.GetCurrentTexture()` returns the next swapchain texture, or an error. Recoverable errors (`Surface timed out`, `is outdated`, `was lost`, `Outdated`) get wrapped in `ErrSurfaceLost` by `wrapSurfaceErr` (`errors.go`) and returned. Everything else returns unwrapped.
3. Create a view on the surface texture.
4. Create a command encoder.
5. Begin a single render pass with one color attachment (`LoadOp = Clear` to `(0.19, 0.24, 0.42, 1.0)`, `StoreOp = Store`) and a depth-stencil attachment (clear to 1.0, store).
6. `e.scene.render(pass)` sets pipeline, bind group, vertex and index buffers, and issues one indexed draw of three indices.
7. End the pass, release it.
8. Finish the encoder into a command buffer, submit it to the queue, call `Present`.

Every transient wgpu object (view, encoder, pass, command buffer) is released with `defer` or an explicit call. The surface texture itself doesn't have a `Release`. wgpu-native owns it.

### `Resize`

`Resize(width, height)` calls `e.gpu.resize(...)` to update `gpu.config.Width/Height` and re-configure the surface, then releases the old depth view and builds a new one at the new dimensions.

This is called from the platform's resize callback (`window.SetSizeCallback` on desktop, `ResizeObserver` on wasm). When the callback fires while the window or canvas is iconified or has zero size, the platform shim is the one that should skip the call.

### `Reconfigure`

`Reconfigure()` re-applies the current `SurfaceConfiguration` without changing dimensions. The platform loops call it after `RenderFrame` returns `ErrSurfaceLost`:

```go
switch err := engine.RenderFrame(delta); {
case err == nil:
case errors.Is(err, render.ErrSurfaceLost):
    engine.Reconfigure()
default:
    log.Fatal(err)
}
```

It is a separate method from `Resize` because the swap chain can be invalidated by events other than resize: monitor change, suspend/resume, GPU driver reset.

### `Release`

`Release()` walks every owned wgpu object in dependency order: scene → depth view → queue → device → adapter → surface. Each release is nil-guarded so a partially-constructed engine can be safely freed. After `Release` the engine must not be used. The pointers are not zeroed, so a double-release would call `Release` on freed wgpu objects.

## `gpu.go`: wgpu plumbing

```go
type gpu struct {
    surface       *wgpu.Surface
    adapter       *wgpu.Adapter
    device        *wgpu.Device
    queue         *wgpu.Queue
    config        *wgpu.SurfaceConfiguration
    surfaceFormat wgpu.TextureFormat
}
```

`newGpu(instance, surface, width, height)` does this:

1. `instance.RequestAdapter` with the surface as `CompatibleSurface`.
2. `adapter.RequestDevice(nil)`. Default device with no requested features or limits.
3. `device.GetQueue()`.
4. Inspect `surface.GetCapabilities(adapter)` and pick the first non-sRGB texture format. `isSrgb` checks against `RGBA8UnormSrgb` and `BGRA8UnormSrgb`. The preference matters: writes from the WGSL fragment shader land linearly on screen instead of going through an sRGB EOTF, which is what you want for the gradient interpolation across the triangle. If no non-sRGB format is available we fall back to `caps.Formats[0]`.
5. Build a `SurfaceConfiguration` with `Usage = RenderAttachment`, the chosen format, present mode `caps.PresentModes[0]` (driver default), alpha mode `caps.AlphaModes[0]`.
6. `surface.Configure(...)`.

`gpu.aspectRatio()` returns `float32(config.Width) / float32(max(config.Height, 1))` so the iconified path can't divide by zero. `gpu.resize(width, height)` updates the config and re-configures the surface. `gpu.reconfigure()` re-applies the config without resizing.

`gpu.createDepthTexture(width, height)` allocates a 2D texture with format `Depth32Float`, `Usage = RenderAttachment | TextureBinding`, builds a view with a default descriptor, releases the texture (the view retains it), and returns the view.

## `errors.go`: recoverable surface errors

```go
var ErrSurfaceLost = errors.New("wgpu surface lost or outdated")

var recoverableSurfaceMessages = []string{
    "Surface timed out",
    "Surface is outdated",
    "Surface was lost",
    "Outdated",
}

func isSurfaceLost(err error) bool { /* substring match */ }
func wrapSurfaceErr(err error) error { /* fmt.Errorf("%w: %v", ErrSurfaceLost, err) when match */ }
```

`wgpu-native` does not expose typed errors for surface state; every error comes back as an opaque string. The four substrings above are stable enough across recent versions to recover from. If a future wgpu-native version introduces a new spelling, this list is the only place to touch. Downstream of `Engine.RenderFrame`, callers use `errors.Is(err, ErrSurfaceLost)` and never match strings themselves.

## `pipeline.go`: the render pipeline

```go
//go:embed shader.wgsl
var shaderSource string

func createPipeline(device *wgpu.Device, surfaceFormat wgpu.TextureFormat, uniform *uniformBinding) (*wgpu.RenderPipeline, error)
```

The pipeline is fixed for the demo:

- **Shader module.** WGSL from `shader.wgsl` embedded at compile time. Both entry points (`vertex_main`, `fragment_main`) live in one module.
- **Pipeline layout.** One bind group layout (the uniform layout produced by `newUniformBinding`); no push constants.
- **Vertex state.** One vertex buffer, stride `unsafe.Sizeof(vertex{})` = 32 bytes, two `Float32x4` attributes at offsets 0 (position) and 16 (color).
- **Primitive state.** `TriangleStrip` topology with `IndexFormatUint32` strip index, `FrontFaceCW`, `CullModeNone`. The topology doesn't matter for a 3-vertex draw, but the index format has to be set for any strip topology to satisfy validation. With `CullModeNone`, the front-face winding doesn't affect visibility.
- **Depth-stencil state.** Format `Depth32Float`, `DepthWriteEnabled = true`, `DepthCompare = Less`, stencil ops all `Keep` with `CompareAlways`.
- **Multisample state.** 1 sample, mask `0xFFFFFFFF`, no alpha-to-coverage.
- **Fragment state.** Single color target at the chosen surface format, alpha blending (`BlendStateAlphaBlending`), full color write mask.

The shader module and pipeline layout are released via `defer` once the render pipeline has its references.

## `projection.go`: clip-space bridge

```go
var ndcZTo01 = mgl32.Mat4{
    1, 0, 0, 0,
    0, 1, 0, 0,
    0, 0, 0.5, 0,
    0, 0, 0.5, 1,
}

func perspectiveZO(fovY, aspect, near, far float32) mgl32.Mat4 {
    return ndcZTo01.Mul4(mgl32.Perspective(fovY, aspect, near, far))
}
```

`mgl32.Perspective` produces an OpenGL-convention perspective matrix: x, y ∈ [-1, 1] with y up, z ∈ [-1, 1] after perspective divide. wgpu wants z ∈ [0, 1] (closer = 0, farther = 1). The `ndcZTo01` constant remaps the depth half-range on the host, so the vertex shader stays `out.position = ubo.mvp * vert.position` with no per-vertex fixup.

Handedness is unchanged. `mgl32.Perspective` is right-handed, `mgl32.LookAtV` (in `scene.update`) is right-handed, and `perspectiveZO` preserves both. The triangle's winding is therefore CCW from the camera, which would be back-facing under `FrontFaceCW`; `CullModeNone` is what keeps it visible.

`perspectiveZO` is mirroring the role of C++'s `glm::perspectiveRH_ZO`: a named composition of library primitives, not a hand-derived formula.

## `uniform.go` + `uniform_native.go` + `uniform_js.go`: uniform upload

The types and constructor live in `uniform.go`. The two suffixed files own only the `update` method.

### `uniformBuffer`

```go
type uniformBuffer struct {
    mvp mgl32.Mat4
}

func (u *uniformBuffer) bytes() []byte {
    return unsafe.Slice((*byte)(unsafe.Pointer(u)), unsafe.Sizeof(*u))
}
```

64 bytes (16 × 4-byte floats), column-major. Matches the WGSL `struct Uniform { mvp: mat4x4<f32>; }` exactly. No padding because the struct is one `mat4x4` and `mat4x4` has 16-byte alignment, satisfied by being at offset 0.

`bytes()` is the only direct `unsafe.Pointer` cast in the package. It exposes the struct as a byte slice without copying, so it can be handed to wgpu's queue write path. The returned slice's lifetime is bounded by the receiver's lifetime; callers must not hold onto it past the `update` call.

### `uniformBinding`

```go
type uniformBinding struct {
    buffer          *wgpu.Buffer
    bindGroup       *wgpu.BindGroup
    bindGroupLayout *wgpu.BindGroupLayout
}

func newUniformBinding(device *wgpu.Device) (*uniformBinding, error)
```

The constructor allocates:

1. An initial buffer with `Usage = Uniform | CopyDst`, contents = bytes of `uniformBuffer{mvp: Ident4()}`.
2. A bind group layout with one binding (binding 0, vertex visibility, uniform type).
3. A bind group binding the buffer at binding 0.

The layout is created once and reused by the render pipeline. On wasm the buffer and bind group are recreated every frame (see below); the layout is stable.

### Desktop upload (`uniform_native.go`)

```go
//go:build !js

func (u *uniformBinding) update(_ *wgpu.Device, queue *wgpu.Queue, data uniformBuffer) error {
    queue.WriteBuffer(u.buffer, 0, data.bytes())
    return nil
}
```

`queue.WriteBuffer` is wgpu-native's standard "small uniform update" path. It copies the bytes into a staging area synchronously (the call returns once the data is owned by wgpu-native), so the caller can let `data` go out of scope immediately. There is no error return; the cgo binding does not surface upload errors.

### Wasm upload (`uniform_js.go`)

```go
//go:build js

func (u *uniformBinding) update(device *wgpu.Device, _ *wgpu.Queue, data uniformBuffer) error {
    newBuf, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
        Label:    "Uniform Buffer",
        Contents: data.bytes(),
        Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
    })
    if err != nil { return fmt.Errorf("create uniform buffer: %w", err) }
    newGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
        Label:   "uniform_bind_group",
        Layout:  u.bindGroupLayout,
        Entries: []wgpu.BindGroupEntry{{Binding: 0, Buffer: newBuf, Offset: 0, Size: uint64(unsafe.Sizeof(data))}},
    })
    if err != nil { newBuf.Release(); return fmt.Errorf("create uniform bind group: %w", err) }
    if u.bindGroup != nil { u.bindGroup.Release() }
    if u.buffer != nil    { u.buffer.Release() }
    u.buffer = newBuf
    u.bindGroup = newGroup
    return nil
}
```

This path exists because of a JS/wasm interaction. `cogentcore/webgpu`'s `Queue.WriteBuffer` on the JS path uses `jsx.BytesToJS`, which constructs a typed-array *view* over wasm linear memory. When Go's runtime grows the heap, the underlying `ArrayBuffer` is detached, and any subsequent view construction throws `Cannot perform Construct on a detached ArrayBuffer`. `CreateBufferInit` instead goes through `js.CopyBytesToJS` (a real copy), which survives heap growth. Recreating the buffer and bind group every frame is wasteful but correct. The bind group layout is stable, so the pipeline never has to be touched.

Errors from `CreateBufferInit` and `CreateBindGroup` are wrapped with `fmt.Errorf("...: %w", err)` so the caller can `errors.Unwrap` to see the wgpu-native message. Failure leaves the previous buffer and bind group in place. The next frame will retry.

## `scene.go`: geometry, uniforms, draw

```go
type vertex struct {
    position [4]float32
    color    [4]float32
}

var vertices = [3]vertex{
    {position: [4]float32{ 1, -1, 0, 1}, color: [4]float32{1, 0, 0, 1}},
    {position: [4]float32{-1, -1, 0, 1}, color: [4]float32{0, 1, 0, 1}},
    {position: [4]float32{ 0,  1, 0, 1}, color: [4]float32{0, 0, 1, 1}},
}

var indices = [3]uint32{0, 1, 2}
```

Three vertices, an index buffer holding `[0, 1, 2]`. The index buffer is redundant for a 3-vertex strip but exercises the `IndexFormatUint32` path.

```go
type scene struct {
    model        mgl32.Mat4
    vertexBuffer *wgpu.Buffer
    indexBuffer  *wgpu.Buffer
    uniform      *uniformBinding
    pipeline     *wgpu.RenderPipeline
}
```

`newScene(device, surfaceFormat)`:

1. Create the vertex buffer (`Usage = Vertex`, contents = `wgpu.ToBytes(vertices[:])`).
2. Create the index buffer (`Usage = Index`, contents = `wgpu.ToBytes(indices[:])`).
3. Create the uniform binding via `newUniformBinding`.
4. Create the render pipeline via `createPipeline`.
5. Initialize `model` to `mgl32.Ident4()`.

`scene.update(device, queue, aspectRatio, deltaTime)`:

```go
projection := perspectiveZO(mgl32.DegToRad(80), aspectRatio, 0.1, 1000.0)
view := mgl32.LookAtV(
    mgl32.Vec3{0, 0, 3}, mgl32.Vec3{0, 0, 0}, mgl32.Vec3{0, 1, 0},
)
s.model = s.model.Mul4(mgl32.HomogRotate3DY(mgl32.DegToRad(30) * deltaTime))
mvp := projection.Mul4(view).Mul4(s.model)
return s.uniform.update(device, queue, uniformBuffer{mvp: mvp})
```

The model rotates around world Y at 30°/sec. The view places the camera at (0, 0, 3) looking at the origin. The projection uses an 80° vertical FOV, near 0.1, far 1000. All matrices are `mgl32.Mat4` column-major; `Mul4` composes left-to-right so `mvp * v` does `M(V(P*v))` in math notation.

Floating-point drift accumulates in `s.model` because we rotate the previous frame's matrix instead of computing the rotation from a scalar angle each frame. For a demo this is fine. Production code would store the angle and rebuild the matrix each frame.

`scene.render(pass)`:

```go
pass.SetPipeline(s.pipeline)
pass.SetBindGroup(0, s.uniform.bindGroup, nil)
pass.SetVertexBuffer(0, s.vertexBuffer, 0, wgpu.WholeSize)
pass.SetIndexBuffer(s.indexBuffer, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
pass.DrawIndexed(uint32(len(indices)), 1, 0, 0, 0)
```

`scene.release()` walks the owned objects in reverse construction order, nil-guarding each release.

## `shader.wgsl`

```wgsl
struct Uniform { mvp: mat4x4<f32> };
@group(0) @binding(0) var<uniform> ubo: Uniform;

struct VertexInput  { @location(0) position: vec4<f32>; @location(1) color: vec4<f32>; };
struct VertexOutput { @builtin(position) position: vec4<f32>; @location(0) color: vec4<f32>; };

@vertex
fn vertex_main(vert: VertexInput) -> VertexOutput {
    var out: VertexOutput;
    out.color = vert.color;
    out.position = ubo.mvp * vert.position;
    return out;
}

@fragment
fn fragment_main(in: VertexOutput) -> @location(0) vec4<f32> {
    return vec4<f32>(in.color);
}
```

The smallest vertex + fragment pair that still does something. The vertex stage applies the precomputed MVP (already in wgpu clip-space convention thanks to `perspectiveZO`) and forwards the per-vertex color. The fragment stage emits the interpolated color. `@builtin(position)` is wgpu's clip-space output. Perspective divide, viewport transform, and depth test all happen with the value as-is.

## Lifecycle invariants

A few rules worth knowing if you build on this:

- `Engine` methods have to be called from the goroutine that created the engine. On desktop that goroutine is the one `init()` locked to its OS thread for GLFW. On wasm there are no other goroutines.
- `RenderFrame`, `Resize`, and `Reconfigure` may interleave but not run concurrently. In practice: `Resize` from a window callback runs synchronously on the main thread between `RenderFrame` calls.
- After `Release`, the engine is unusable. `Release` is idempotent against nil owned objects but does not zero its own pointers, so a double-release would call `Release` on freed wgpu objects.
- `ErrSurfaceLost` wraps the underlying wgpu error with `%w`; the caller can `errors.Unwrap` to read the original message. Recovery is `Reconfigure` followed by retrying on the next frame.
