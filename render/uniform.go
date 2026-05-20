package render

import (
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

type uniformBuffer struct {
	mvp mgl32.Mat4
}

func (u *uniformBuffer) bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(u)), unsafe.Sizeof(*u))
}

type uniformBinding struct {
	buffer          *wgpu.Buffer
	bindGroup       *wgpu.BindGroup
	bindGroupLayout *wgpu.BindGroupLayout
}

func newUniformBinding(device *wgpu.Device) (*uniformBinding, error) {
	zero := uniformBuffer{mvp: mgl32.Ident4()}
	buffer, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Uniform Buffer",
		Contents: zero.bytes(),
		Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return nil, err
	}

	layout, err := device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Label: "uniform_bind_group_layout",
		Entries: []wgpu.BindGroupLayoutEntry{{
			Binding:    0,
			Visibility: wgpu.ShaderStageVertex,
			Buffer: wgpu.BufferBindingLayout{
				Type:             wgpu.BufferBindingTypeUniform,
				HasDynamicOffset: false,
				MinBindingSize:   0,
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	group, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "uniform_bind_group",
		Layout: layout,
		Entries: []wgpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  buffer,
			Offset:  0,
			Size:    uint64(unsafe.Sizeof(zero)),
		}},
	})
	if err != nil {
		return nil, err
	}

	return &uniformBinding{buffer: buffer, bindGroup: group, bindGroupLayout: layout}, nil
}

func (u *uniformBinding) release() {
	if u.bindGroup != nil {
		u.bindGroup.Release()
	}
	if u.bindGroupLayout != nil {
		u.bindGroupLayout.Release()
	}
	if u.buffer != nil {
		u.buffer.Release()
	}
}
