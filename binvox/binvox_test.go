package binvox

import (
	"image"
	"image/color"
	"os"
	"testing"

	"github.com/gmlewis/irmf-slicer/v3/irmf"
	"github.com/gmlewis/stldice/v4/binvox"
)

type mockSlicer struct {
	nx, ny, nz int
	matName    string
}

func (m *mockSlicer) NumMaterials() int { return 1 }
func (m *mockSlicer) MaterialName(materialNum int) string {
	if m.matName != "" {
		return m.matName
	}
	return "mat1"
}
func (m *mockSlicer) MBB() (min, max [3]float32) {
	return [3]float32{0, 0, 0}, [3]float32{float32(m.nx), float32(m.ny), float32(m.nz)}
}
func (m *mockSlicer) PrepareRenderZ() error { return nil }
func (m *mockSlicer) RenderZSlices(materialNum int, sp irmf.ZSliceProcessor, order irmf.Order) error {
	img := image.NewRGBA(image.Rect(0, 0, m.nx, m.ny))
	for y := 0; y < m.ny; y++ {
		for x := 0; x < m.nx; x++ {
			img.Set(x, y, color.White)
		}
	}

	for i := 0; i < m.nz; i++ {
		if err := sp.ProcessZSlice(i, float32(i)+0.5, 0.5, img); err != nil {
			return err
		}
	}
	return nil
}
func (m *mockSlicer) NumXSlices() int { return m.nx }
func (m *mockSlicer) NumYSlices() int { return m.ny }
func (m *mockSlicer) NumZSlices() int { return m.nz }

func TestSliceSolid(t *testing.T) {
	slicer := &mockSlicer{nx: 3, ny: 3, nz: 3, matName: "test material"}

	filename := "test-solid"
	err := Slice(filename, slicer)
	if err != nil {
		t.Fatalf("Slice failed: %v", err)
	}
	// "test material" should become "test-material"
	realFilename := "test-solid-mat01-test-material.binvox"
	t.Cleanup(func() {
		os.Remove(realFilename)
	})

	b, err := binvox.Read(realFilename, 0, 0, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	count := len(b.WhiteVoxels)
	expected := 27
	if count != expected {
		t.Errorf("Expected %v voxels, got %v", expected, count)
	}

	if b.NX != 3 || b.NY != 3 || b.NZ != 3 {
		t.Errorf("Expected dimensions 3x3x3, got %vx%vx%v", b.NX, b.NY, b.NZ)
	}
}
