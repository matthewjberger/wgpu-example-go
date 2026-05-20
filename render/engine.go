// Package render owns the WGPU surface, swap chain, depth target, and scene
// for the spinning-triangle demo. Construct an [Engine] with [New], drive it
// with [Engine.RenderFrame] each frame, and free its GPU objects with
// [Engine.Release] on shutdown.
package render

import "github.com/cogentcore/webgpu/wgpu"

// Engine owns every WGPU object needed to draw one frame: the surface,
// adapter, device, queue, depth texture, and scene. It is not safe for
// concurrent use; call its methods from the same goroutine.
type Engine struct {
	gpu              *gpu
	depthTextureView *wgpu.TextureView
	scene            *scene
}

// New acquires an adapter and device from instance, configures surface at
// the given dimensions, and builds the default scene. The caller owns the
// returned [Engine] and must call [Engine.Release] to free it.
func New(instance *wgpu.Instance, surface *wgpu.Surface, width, height uint32) (*Engine, error) {
	g, err := newGpu(instance, surface, width, height)
	if err != nil {
		return nil, err
	}
	depthView, err := g.createDepthTexture(g.config.Width, g.config.Height)
	if err != nil {
		return nil, err
	}
	s, err := newScene(g.device, g.surfaceFormat)
	if err != nil {
		return nil, err
	}
	return &Engine{gpu: g, depthTextureView: depthView, scene: s}, nil
}

// Resize re-configures the surface and rebuilds the depth texture at the new
// dimensions. Call this from a window/canvas resize callback.
func (e *Engine) Resize(width, height uint32) error {
	e.gpu.resize(width, height)
	if e.depthTextureView != nil {
		e.depthTextureView.Release()
	}
	view, err := e.gpu.createDepthTexture(width, height)
	if err != nil {
		return err
	}
	e.depthTextureView = view
	return nil
}

// Reconfigure re-applies the current surface configuration. Call this after
// [Engine.RenderFrame] returns an error wrapping [ErrSurfaceLost] to rebuild
// the swap chain.
func (e *Engine) Reconfigure() {
	e.gpu.reconfigure()
}

// RenderFrame advances the scene by deltaTime seconds and submits one frame
// to the GPU. Recoverable surface errors are wrapped in [ErrSurfaceLost]; the
// caller should match with [errors.Is] and decide whether to retry after
// [Engine.Reconfigure].
func (e *Engine) RenderFrame(deltaTime float32) error {
	if err := e.scene.update(e.gpu.device, e.gpu.queue, e.gpu.aspectRatio(), deltaTime); err != nil {
		return err
	}

	surfaceTex, err := e.gpu.surface.GetCurrentTexture()
	if err != nil {
		return wrapSurfaceErr(err)
	}

	view, err := surfaceTex.CreateView(nil)
	if err != nil {
		return err
	}
	defer view.Release()

	encoder, err := e.gpu.device.CreateCommandEncoder(&wgpu.CommandEncoderDescriptor{Label: "Render Encoder"})
	if err != nil {
		return err
	}
	defer encoder.Release()

	pass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		Label: "Render Pass",
		ColorAttachments: []wgpu.RenderPassColorAttachment{{
			View:       view,
			LoadOp:     wgpu.LoadOpClear,
			StoreOp:    wgpu.StoreOpStore,
			ClearValue: wgpu.Color{R: 0.19, G: 0.24, B: 0.42, A: 1.0},
		}},
		DepthStencilAttachment: &wgpu.RenderPassDepthStencilAttachment{
			View:            e.depthTextureView,
			DepthLoadOp:     wgpu.LoadOpClear,
			DepthStoreOp:    wgpu.StoreOpStore,
			DepthClearValue: 1.0,
		},
	})

	e.scene.render(pass)
	pass.End()
	pass.Release()

	cmd, err := encoder.Finish(nil)
	if err != nil {
		return err
	}
	defer cmd.Release()

	e.gpu.queue.Submit(cmd)
	e.gpu.surface.Present()
	return nil
}

// Release frees every WGPU object owned by the engine in dependency order
// (scene → depth view → queue → device → adapter → surface). Safe to call on
// a partially-constructed [Engine].
func (e *Engine) Release() {
	if e.scene != nil {
		e.scene.release()
	}
	if e.depthTextureView != nil {
		e.depthTextureView.Release()
	}
	if e.gpu != nil {
		if e.gpu.queue != nil {
			e.gpu.queue.Release()
		}
		if e.gpu.device != nil {
			e.gpu.device.Release()
		}
		if e.gpu.adapter != nil {
			e.gpu.adapter.Release()
		}
		if e.gpu.surface != nil {
			e.gpu.surface.Release()
		}
	}
}
