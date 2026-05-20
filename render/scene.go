package render

import (
	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

type vertex struct {
	position [4]float32
	color    [4]float32
}

var vertices = [3]vertex{
	{position: [4]float32{1.0, -1.0, 0.0, 1.0}, color: [4]float32{1.0, 0.0, 0.0, 1.0}},
	{position: [4]float32{-1.0, -1.0, 0.0, 1.0}, color: [4]float32{0.0, 1.0, 0.0, 1.0}},
	{position: [4]float32{0.0, 1.0, 0.0, 1.0}, color: [4]float32{0.0, 0.0, 1.0, 1.0}},
}

var indices = [3]uint32{0, 1, 2}

type scene struct {
	model        mgl32.Mat4
	vertexBuffer *wgpu.Buffer
	indexBuffer  *wgpu.Buffer
	uniform      *uniformBinding
	pipeline     *wgpu.RenderPipeline
}

func newScene(device *wgpu.Device, surfaceFormat wgpu.TextureFormat) (*scene, error) {
	vertexBuffer, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Vertex Buffer",
		Contents: wgpu.ToBytes(vertices[:]),
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		return nil, err
	}

	indexBuffer, err := device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Index Buffer",
		Contents: wgpu.ToBytes(indices[:]),
		Usage:    wgpu.BufferUsageIndex,
	})
	if err != nil {
		return nil, err
	}

	uniform, err := newUniformBinding(device)
	if err != nil {
		return nil, err
	}

	pipeline, err := createPipeline(device, surfaceFormat, uniform)
	if err != nil {
		return nil, err
	}

	return &scene{
		model:        mgl32.Ident4(),
		vertexBuffer: vertexBuffer,
		indexBuffer:  indexBuffer,
		uniform:      uniform,
		pipeline:     pipeline,
	}, nil
}

func (s *scene) update(device *wgpu.Device, queue *wgpu.Queue, aspectRatio, deltaTime float32) error {
	projection := perspectiveZO(mgl32.DegToRad(80), aspectRatio, 0.1, 1000.0)
	view := mgl32.LookAtV(
		mgl32.Vec3{0, 0, 3},
		mgl32.Vec3{0, 0, 0},
		mgl32.Vec3{0, 1, 0},
	)
	s.model = s.model.Mul4(mgl32.HomogRotate3DY(mgl32.DegToRad(30) * deltaTime))
	mvp := projection.Mul4(view).Mul4(s.model)
	return s.uniform.update(device, queue, uniformBuffer{mvp: mvp})
}

func (s *scene) render(pass *wgpu.RenderPassEncoder) {
	pass.SetPipeline(s.pipeline)
	pass.SetBindGroup(0, s.uniform.bindGroup, nil)
	pass.SetVertexBuffer(0, s.vertexBuffer, 0, wgpu.WholeSize)
	pass.SetIndexBuffer(s.indexBuffer, wgpu.IndexFormatUint32, 0, wgpu.WholeSize)
	pass.DrawIndexed(uint32(len(indices)), 1, 0, 0, 0)
}

func (s *scene) release() {
	if s.pipeline != nil {
		s.pipeline.Release()
	}
	if s.uniform != nil {
		s.uniform.release()
	}
	if s.indexBuffer != nil {
		s.indexBuffer.Release()
	}
	if s.vertexBuffer != nil {
		s.vertexBuffer.Release()
	}
}
