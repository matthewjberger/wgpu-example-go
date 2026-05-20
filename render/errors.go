package render

import (
	"errors"
	"fmt"
	"strings"
)

// ErrSurfaceLost is returned (wrapped) by [Engine.RenderFrame] when the swap
// chain needs to be rebuilt — typically after a resize, monitor change, or
// suspend/resume. Recover by calling [Engine.Reconfigure] and retrying on the
// next frame. Match it with [errors.Is].
var ErrSurfaceLost = errors.New("wgpu surface lost or outdated")

// recoverableSurfaceMessages are wgpu-native error substrings that indicate
// the swap chain is gone and the surface needs to be reconfigured. wgpu-native
// returns these as opaque strings, so substring-matching is the only handle
// we have; anyone updating the wgpu binding edits this list rather than user
// code.
var recoverableSurfaceMessages = []string{
	"Surface timed out",
	"Surface is outdated",
	"Surface was lost",
	"Outdated",
}

func isSurfaceLost(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	for _, substring := range recoverableSurfaceMessages {
		if strings.Contains(message, substring) {
			return true
		}
	}
	return false
}

// wrapSurfaceErr returns err wrapped in [ErrSurfaceLost] when it matches one
// of [recoverableSurfaceMessages], otherwise returns err unchanged.
func wrapSurfaceErr(err error) error {
	if isSurfaceLost(err) {
		return fmt.Errorf("%w: %v", ErrSurfaceLost, err)
	}
	return err
}
