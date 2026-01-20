package irmf

import (
	"image"

	"github.com/go-gl/mathgl/mgl32"
)

// Renderer represents a renderer backend.
type Renderer interface {
	Init(width, height int, view bool) error
	Prepare(irmf *IRMF, vec3Str string, planeVertices []float32, projection, camera, model mgl32.Mat4) error
	Render(sliceDepth float32, materialNum int) (image.Image, error)
	Close()
}
