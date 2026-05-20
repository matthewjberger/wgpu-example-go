//go:build js

package render

import (
	"fmt"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

// On JS the cogentcore writeBuffer path uses jsx.BytesToJS, which builds a
// typed-array view over wasm linear memory. When Go's runtime grows the wasm
// memory the underlying ArrayBuffer becomes detached and subsequent
// constructions throw "Cannot perform Construct on a detached ArrayBuffer".
// CreateBufferInit copies through js.CopyBytesToJS, which is safe, so we
// recreate the uniform buffer + bind group each frame.
func (u *uniformBinding) update(device *wgpu.Device, _ *wgpu.Queue, data uniformBuffer) error {
	newBuf, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Uniform Buffer",
		Contents: data.bytes(),
		Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("create uniform buffer: %w", err)
	}

	newGroup, err := device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Label:  "uniform_bind_group",
		Layout: u.bindGroupLayout,
		Entries: []wgpu.BindGroupEntry{{
			Binding: 0,
			Buffer:  newBuf,
			Offset:  0,
			Size:    uint64(unsafe.Sizeof(data)),
		}},
	})
	if err != nil {
		newBuf.Release()
		return fmt.Errorf("create uniform bind group: %w", err)
	}

	if u.bindGroup != nil {
		u.bindGroup.Release()
	}
	if u.buffer != nil {
		u.buffer.Release()
	}
	u.buffer = newBuf
	u.bindGroup = newGroup
	return nil
}
