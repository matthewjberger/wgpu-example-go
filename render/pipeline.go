package render

import (
	_ "embed"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

//go:embed shader.wgsl
var shaderSource string

func createPipeline(device *wgpu.Device, surfaceFormat wgpu.TextureFormat, uniform *uniformBinding) (*wgpu.RenderPipeline, error) {
	shader, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{Code: shaderSource},
	})
	if err != nil {
		return nil, err
	}
	defer shader.Release()

	layout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{uniform.bindGroupLayout},
	})
	if err != nil {
		return nil, err
	}
	defer layout.Release()

	return device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: layout,
		Vertex: wgpu.VertexState{
			Module:     shader,
			EntryPoint: "vertex_main",
			Buffers: []wgpu.VertexBufferLayout{{
				ArrayStride: uint64(unsafe.Sizeof(vertex{})),
				StepMode:    wgpu.VertexStepModeVertex,
				Attributes: []wgpu.VertexAttribute{
					{Format: wgpu.VertexFormatFloat32x4, Offset: 0, ShaderLocation: 0},
					{Format: wgpu.VertexFormatFloat32x4, Offset: 16, ShaderLocation: 1},
				},
			}},
		},
		Primitive: wgpu.PrimitiveState{
			Topology:         wgpu.PrimitiveTopologyTriangleStrip,
			StripIndexFormat: wgpu.IndexFormatUint32,
			FrontFace:        wgpu.FrontFaceCW,
			CullMode:         wgpu.CullModeNone,
		},
		DepthStencil: &wgpu.DepthStencilState{
			Format:            depthFormat,
			DepthWriteEnabled: true,
			DepthCompare:      wgpu.CompareFunctionLess,
			StencilFront: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
			StencilBack: wgpu.StencilFaceState{
				Compare:     wgpu.CompareFunctionAlways,
				FailOp:      wgpu.StencilOperationKeep,
				DepthFailOp: wgpu.StencilOperationKeep,
				PassOp:      wgpu.StencilOperationKeep,
			},
		},
		Multisample: wgpu.MultisampleState{
			Count:                  1,
			Mask:                   0xFFFFFFFF,
			AlphaToCoverageEnabled: false,
		},
		Fragment: &wgpu.FragmentState{
			Module:     shader,
			EntryPoint: "fragment_main",
			Targets: []wgpu.ColorTargetState{{
				Format:    surfaceFormat,
				Blend:     &wgpu.BlendStateAlphaBlending,
				WriteMask: wgpu.ColorWriteMaskAll,
			}},
		},
	})
}
