//go:build !js

package main

import (
	"errors"
	"log"
	"os"
	"runtime"
	"time"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/cogentcore/webgpu/wgpuglfw"
	"github.com/go-gl/glfw/v3.3/glfw"

	"wgpu-example-go/render"
)

// init locks the main goroutine to the OS thread so GLFW's window and event
// APIs (which must run on the thread that called Init) stay there.
func init() {
	runtime.LockOSThread()
}

// setupLogging maps WGPU_LOG_LEVEL to wgpu-native's log level enum.
// Unrecognized values leave logging at its default (off).
func setupLogging() {
	switch os.Getenv("WGPU_LOG_LEVEL") {
	case "OFF":
		wgpu.SetLogLevel(wgpu.LogLevelOff)
	case "ERROR":
		wgpu.SetLogLevel(wgpu.LogLevelError)
	case "WARN":
		wgpu.SetLogLevel(wgpu.LogLevelWarn)
	case "INFO":
		wgpu.SetLogLevel(wgpu.LogLevelInfo)
	case "DEBUG":
		wgpu.SetLogLevel(wgpu.LogLevelDebug)
	case "TRACE":
		wgpu.SetLogLevel(wgpu.LogLevelTrace)
	}
}

func main() {
	setupLogging()

	if err := glfw.Init(); err != nil {
		log.Fatal(err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ClientAPI, glfw.NoAPI)
	window, err := glfw.CreateWindow(1280, 720, "Standalone GLFW/Wgpu Example", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer window.Destroy()

	instance := wgpu.CreateInstance(nil)
	defer instance.Release()

	surface := instance.CreateSurface(wgpuglfw.GetSurfaceDescriptor(window))

	width, height := window.GetSize()
	engine, err := render.New(instance, surface, uint32(width), uint32(height))
	if err != nil {
		log.Fatal(err)
	}
	defer engine.Release()

	window.SetSizeCallback(func(_ *glfw.Window, width, height int) {
		if width > 0 && height > 0 {
			if err := engine.Resize(uint32(width), uint32(height)); err != nil {
				log.Printf("resize error: %v", err)
			}
		}
	})

	window.SetKeyCallback(func(w *glfw.Window, key glfw.Key, _ int, action glfw.Action, _ glfw.ModifierKey) {
		if key == glfw.KeyEscape && action == glfw.Press {
			w.SetShouldClose(true)
		}
	})

	last := time.Now()
	for !window.ShouldClose() {
		glfw.PollEvents()

		now := time.Now()
		delta := float32(now.Sub(last).Seconds())
		last = now

		switch err := engine.RenderFrame(delta); {
		case err == nil:
		case errors.Is(err, render.ErrSurfaceLost):
			engine.Reconfigure()
		default:
			log.Fatal(err)
		}
	}
}
