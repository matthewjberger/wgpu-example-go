//go:build !js

package render

import "github.com/cogentcore/webgpu/wgpu"

func (u *uniformBinding) update(_ *wgpu.Device, queue *wgpu.Queue, data uniformBuffer) error {
	queue.WriteBuffer(u.buffer, 0, data.bytes())
	return nil
}
