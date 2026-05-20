package render

import "github.com/cogentcore/webgpu/wgpu"

const depthFormat = wgpu.TextureFormatDepth32Float

type gpu struct {
	surface       *wgpu.Surface
	adapter       *wgpu.Adapter
	device        *wgpu.Device
	queue         *wgpu.Queue
	config        *wgpu.SurfaceConfiguration
	surfaceFormat wgpu.TextureFormat
}

func newGpu(instance *wgpu.Instance, surface *wgpu.Surface, width, height uint32) (*gpu, error) {
	g := &gpu{surface: surface}

	adapter, err := instance.RequestAdapter(&wgpu.RequestAdapterOptions{
		CompatibleSurface: g.surface,
	})
	if err != nil {
		return nil, err
	}
	g.adapter = adapter

	device, err := adapter.RequestDevice(nil)
	if err != nil {
		return nil, err
	}
	g.device = device
	g.queue = device.GetQueue()

	caps := g.surface.GetCapabilities(adapter)

	g.surfaceFormat = caps.Formats[0]
	for _, format := range caps.Formats {
		if !isSrgb(format) {
			g.surfaceFormat = format
			break
		}
	}

	g.config = &wgpu.SurfaceConfiguration{
		Usage:       wgpu.TextureUsageRenderAttachment,
		Format:      g.surfaceFormat,
		Width:       width,
		Height:      height,
		PresentMode: caps.PresentModes[0],
		AlphaMode:   caps.AlphaModes[0],
	}

	g.surface.Configure(g.adapter, g.device, g.config)

	return g, nil
}

func (g *gpu) aspectRatio() float32 {
	height := g.config.Height
	if height < 1 {
		height = 1
	}
	return float32(g.config.Width) / float32(height)
}

func (g *gpu) resize(width, height uint32) {
	g.config.Width = width
	g.config.Height = height
	g.surface.Configure(g.adapter, g.device, g.config)
}

func (g *gpu) reconfigure() {
	g.surface.Configure(g.adapter, g.device, g.config)
}

func (g *gpu) createDepthTexture(width, height uint32) (*wgpu.TextureView, error) {
	tex, err := g.device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Depth Texture",
		Size: wgpu.Extent3D{
			Width:              width,
			Height:             height,
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        depthFormat,
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageTextureBinding,
	})
	if err != nil {
		return nil, err
	}
	defer tex.Release()
	return tex.CreateView(nil)
}

func isSrgb(f wgpu.TextureFormat) bool {
	switch f {
	case wgpu.TextureFormatRGBA8UnormSrgb,
		wgpu.TextureFormatBGRA8UnormSrgb:
		return true
	}
	return false
}
