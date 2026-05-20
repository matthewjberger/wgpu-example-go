//go:build js

package main

import (
	"fmt"
	"syscall/js"
	"time"

	"github.com/cogentcore/webgpu/wgpu"

	"wgpu-example-go/render"
)

func main() {
	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", "canvas")
	if !canvas.Truthy() {
		js.Global().Get("console").Call("error", "no <canvas id=\"canvas\"> found")
		return
	}

	width := uint32(canvas.Get("width").Int())
	height := uint32(canvas.Get("height").Int())

	instance := wgpu.CreateInstance(nil)
	if instance == nil {
		js.Global().Get("console").Call("error", "WebGPU not supported in this browser")
		return
	}

	surface := instance.CreateSurface(&wgpu.SurfaceDescriptor{Canvas: canvas})

	engine, err := render.New(instance, surface, width, height)
	if err != nil {
		js.Global().Get("console").Call("error", "renderer init failed: "+err.Error())
		return
	}

	// resizeFunc and frame are deliberately kept alive for the page lifetime
	// (main never returns), so we never call (js.Func).Release on them.
	resizeFunc := js.FuncOf(func(_ js.Value, args []js.Value) any {
		entries := args[0]
		if entries.Length() == 0 {
			return nil
		}
		entry := entries.Index(0)
		contentBoxSize := entry.Get("contentBoxSize")
		if !contentBoxSize.Truthy() || contentBoxSize.Length() == 0 {
			return nil
		}
		box := contentBoxSize.Index(0)
		w := uint32(box.Get("inlineSize").Int())
		h := uint32(box.Get("blockSize").Int())
		if w == 0 || h == 0 {
			return nil
		}
		canvas.Set("width", w)
		canvas.Set("height", h)
		if err := engine.Resize(w, h); err != nil {
			js.Global().Get("console").Call("error", "resize failed: "+err.Error())
		}
		return nil
	})
	resizeObserver := js.Global().Get("ResizeObserver").New(resizeFunc)
	resizeObserver.Call("observe", canvas)

	last := time.Now()
	frameCount := 0
	lastFpsLog := time.Now()
	var frame js.Func
	frame = js.FuncOf(func(_ js.Value, _ []js.Value) any {
		now := time.Now()
		delta := float32(now.Sub(last).Seconds())
		last = now

		if err := engine.RenderFrame(delta); err != nil {
			js.Global().Get("console").Call("error", "render error: "+err.Error())
		}

		frameCount++
		if now.Sub(lastFpsLog) >= time.Second {
			js.Global().Get("console").Call("log", fmt.Sprintf("fps=%d delta=%.4f", frameCount, delta))
			frameCount = 0
			lastFpsLog = now
		}

		js.Global().Call("requestAnimationFrame", frame)
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)

	select {}
}
