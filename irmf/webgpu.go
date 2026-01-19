package irmf

import (
	"fmt"
	"image"
	"strings"

	"github.com/cogentcore/webgpu/wgpu"
	"github.com/go-gl/mathgl/mgl32"
)

// WebGPURenderer is a renderer implementation using WebGPU.
type WebGPURenderer struct {
	width       int
	height      int
	view        bool
	bytesPerRow uint32

	instance *wgpu.Instance
	adapter  *wgpu.Adapter
	device   *wgpu.Device
	queue    *wgpu.Queue

	pipeline      *wgpu.RenderPipeline
	bindGroup     *wgpu.BindGroup
	vertexBuffer  *wgpu.Buffer
	uniformBuffer *wgpu.Buffer
	readBuffer    *wgpu.Buffer
	targetTexture *wgpu.Texture
	targetView    *wgpu.TextureView

	irmf *IRMF
}

func (r *WebGPURenderer) Init(width, height int, view bool) error {
	r.width = width
	r.height = height
	r.view = view

	if r.instance == nil {
		r.instance = wgpu.CreateInstance(nil)
		if r.instance == nil {
			return fmt.Errorf("failed to create wgpu instance")
		}

		var err error
		r.adapter, err = r.instance.RequestAdapter(&wgpu.RequestAdapterOptions{})
		if err != nil {
			return fmt.Errorf("failed to request wgpu adapter: %w", err)
		}

		r.device, err = r.adapter.RequestDevice(nil)
		if err != nil {
			return fmt.Errorf("failed to request wgpu device: %w", err)
		}

		r.queue = r.device.GetQueue()
	}

	return nil
}

func (r *WebGPURenderer) Prepare(irmf *IRMF, vec3Str string, planeVertices []float32, projection, camera, model mgl32.Mat4) error {
	r.irmf = irmf

	// Define WGSL shader
	shaderSource := wgslVertexShader + wgslFSHeader + irmf.Shader + genWGSLFooter(len(irmf.Materials), vec3Str)

	shaderModule, err := r.device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
			Code: shaderSource,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create shader module: %w", err)
	}
	defer shaderModule.Release()

	// Create Buffers
	r.vertexBuffer, err = r.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Vertex Buffer",
		Contents: wgpu.ToBytes(planeVertices),
		Usage:    wgpu.BufferUsageVertex,
	})
	if err != nil {
		return fmt.Errorf("failed to create vertex buffer: %w", err)
	}

	// Uniforms
	uniformData := make([]float32, 16*3+4) // 3 matrices + u_slice + u_materialNum + 2 floats padding
	copy(uniformData[0:16], projection[:])
	copy(uniformData[16:32], camera[:])
	copy(uniformData[32:48], model[:])
	uniformData[48] = 0.0 // u_slice
	uniformData[49] = 1.0 // u_materialNum

	r.uniformBuffer, err = r.device.CreateBufferInit(&wgpu.BufferInitDescriptor{
		Label:    "Uniform Buffer",
		Contents: wgpu.ToBytes(uniformData),
		Usage:    wgpu.BufferUsageUniform | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create uniform buffer: %w", err)
	}

	// Bind Group Layout
	bindGroupLayout, err := r.device.CreateBindGroupLayout(&wgpu.BindGroupLayoutDescriptor{
		Entries: []wgpu.BindGroupLayoutEntry{
			{
				Binding:    0,
				Visibility: wgpu.ShaderStageVertex | wgpu.ShaderStageFragment,
				Buffer: wgpu.BufferBindingLayout{
					Type: wgpu.BufferBindingTypeUniform,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create bind group layout: %w", err)
	}
	defer bindGroupLayout.Release()

	r.bindGroup, err = r.device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: bindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{
				Binding: 0,
				Buffer:  r.uniformBuffer,
				Size:    uint64(len(uniformData) * 4),
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create bind group: %w", err)
	}

	pipelineLayout, err := r.device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: []*wgpu.BindGroupLayout{bindGroupLayout},
	})
	if err != nil {
		return fmt.Errorf("failed to create pipeline layout: %w", err)
	}
	defer pipelineLayout.Release()

	// Target Texture for offscreen rendering
	r.targetTexture, err = r.device.CreateTexture(&wgpu.TextureDescriptor{
		Label: "Target Texture",
		Size: wgpu.Extent3D{
			Width:              uint32(r.width),
			Height:             uint32(r.height),
			DepthOrArrayLayers: 1,
		},
		MipLevelCount: 1,
		SampleCount:   1,
		Dimension:     wgpu.TextureDimension2D,
		Format:        wgpu.TextureFormatRGBA8Unorm,
		Usage:         wgpu.TextureUsageRenderAttachment | wgpu.TextureUsageCopySrc,
	})
	if err != nil {
		return fmt.Errorf("failed to create target texture: %w", err)
	}
	r.targetView, err = r.targetTexture.CreateView(nil)
	if err != nil {
		return fmt.Errorf("failed to create texture view: %w", err)
	}

	// Pipeline
	r.pipeline, err = r.device.CreateRenderPipeline(&wgpu.RenderPipelineDescriptor{
		Layout: pipelineLayout,
		Vertex: wgpu.VertexState{
			Module:     shaderModule,
			EntryPoint: "vs_main",
			Buffers: []wgpu.VertexBufferLayout{
				{
					ArrayStride: 5 * 4,
					Attributes: []wgpu.VertexAttribute{
						{
							Format:         wgpu.VertexFormatFloat32x3,
							Offset:         0,
							ShaderLocation: 0,
						},
					},
				},
			},
		},
		Fragment: &wgpu.FragmentState{
			Module:     shaderModule,
			EntryPoint: "fs_main",
			Targets: []wgpu.ColorTargetState{
				{
					Format:    wgpu.TextureFormatRGBA8Unorm,
					WriteMask: wgpu.ColorWriteMaskAll,
				},
			},
		},
		Primitive: wgpu.PrimitiveState{
			Topology: wgpu.PrimitiveTopologyTriangleList,
		},
		Multisample: wgpu.MultisampleState{
			Count: 1,
			Mask:  0xFFFFFFFF,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create render pipeline: %w", err)
	}

	// Read buffer for capturing results
	r.bytesPerRow = (uint32(r.width*4) + 255) &^ 255
	r.readBuffer, err = r.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "Read Buffer",
		Size:  uint64(r.bytesPerRow * uint32(r.height)),
		Usage: wgpu.BufferUsageMapRead | wgpu.BufferUsageCopyDst,
	})
	if err != nil {
		return fmt.Errorf("failed to create read buffer: %w", err)
	}

	return nil
}

func (r *WebGPURenderer) Render(sliceDepth float32, materialNum int) (image.Image, error) {
	// Update Uniforms
	uniformData := []float32{sliceDepth, float32(materialNum)}
	r.queue.WriteBuffer(r.uniformBuffer, 48*4, wgpu.ToBytes(uniformData))

	encoder, err := r.device.CreateCommandEncoder(nil)
	if err != nil {
		return nil, err
	}

	renderPass := encoder.BeginRenderPass(&wgpu.RenderPassDescriptor{
		ColorAttachments: []wgpu.RenderPassColorAttachment{
			{
				View:       r.targetView,
				LoadOp:     wgpu.LoadOpClear,
				StoreOp:    wgpu.StoreOpStore,
				ClearValue: wgpu.Color{R: 0, G: 0, B: 0, A: 0},
			},
		},
	})
	renderPass.SetPipeline(r.pipeline)
	renderPass.SetBindGroup(0, r.bindGroup, nil)
	renderPass.SetVertexBuffer(0, r.vertexBuffer, 0, r.vertexBuffer.GetSize())
	renderPass.Draw(6, 1, 0, 0)
	if err := renderPass.End(); err != nil {
		renderPass.Release()
		return nil, err
	}
	renderPass.Release()

	// Copy texture to read buffer
	encoder.CopyTextureToBuffer(
		r.targetTexture.AsImageCopy(),
		&wgpu.ImageCopyBuffer{
			Buffer: r.readBuffer,
			Layout: wgpu.TextureDataLayout{
				Offset:       0,
				BytesPerRow:  r.bytesPerRow,
				RowsPerImage: uint32(r.height),
			},
		},
		&wgpu.Extent3D{
			Width:              uint32(r.width),
			Height:             uint32(r.height),
			DepthOrArrayLayers: 1,
		},
	)

	commandBuffer, err := encoder.Finish(nil)
	if err != nil {
		return nil, err
	}
	r.queue.Submit(commandBuffer)
	commandBuffer.Release()
	encoder.Release()

	// Map buffer and read image
	done := make(chan struct{})
	var mapStatus wgpu.BufferMapAsyncStatus
	r.readBuffer.MapAsync(wgpu.MapModeRead, 0, uint64(r.bytesPerRow*uint32(r.height)), func(status wgpu.BufferMapAsyncStatus) {
		mapStatus = status
		close(done)
	})

	for {
		r.device.Poll(false, nil)
		select {
		case <-done:
			goto mapped
		default:
			// continue polling
		}
	}

mapped:
	if mapStatus != wgpu.BufferMapAsyncStatusSuccess {
		return nil, fmt.Errorf("failed to map read buffer: %v", mapStatus)
	}

	data := r.readBuffer.GetMappedRange(0, uint(r.bytesPerRow*uint32(r.height)))
	rgba := &image.RGBA{
		Pix:    make([]uint8, r.width*r.height*4),
		Stride: r.width * 4,
		Rect:   image.Rect(0, 0, r.width, r.height),
	}
	for y := 0; y < r.height; y++ {
		srcStart := uint32(y) * r.bytesPerRow
		srcEnd := srcStart + uint32(r.width*4)
		destStart := y * r.width * 4
		copy(rgba.Pix[destStart:destStart+r.width*4], data[srcStart:srcEnd])
	}
	r.readBuffer.Unmap()

	return rgba, nil
}

func (r *WebGPURenderer) Close() {
	if r.readBuffer != nil {
		r.readBuffer.Release()
	}
	if r.targetTexture != nil {
		r.targetTexture.Release()
	}
	if r.targetView != nil {
		r.targetView.Release()
	}
	if r.vertexBuffer != nil {
		r.vertexBuffer.Release()
	}
	if r.uniformBuffer != nil {
		r.uniformBuffer.Release()
	}
	if r.pipeline != nil {
		r.pipeline.Release()
	}
	if r.bindGroup != nil {
		r.bindGroup.Release()
	}
	if r.device != nil {
		r.device.Release()
	}
	if r.adapter != nil {
		r.adapter.Release()
	}
	if r.instance != nil {
		r.instance.Release()
	}
}

const wgslVertexShader = `
struct Uniforms {
    projection: mat4x4f,
    camera: mat4x4f,
    model: mat4x4f,
    u_slice: f32,
    u_materialNum: f32,
};

@group(0) @binding(0) var<uniform> uniforms: Uniforms;

struct VertexOutput {
    @builtin(position) position: vec4f,
    @location(0) fragVert: vec3f,
};

@vertex
fn vs_main(@location(0) vert: vec3f) -> VertexOutput {
    var out: VertexOutput;
    out.position = uniforms.projection * uniforms.camera * uniforms.model * vec4f(vert, 1.0);
    out.fragVert = vert;
    return out;
}
`

const wgslFSHeader = `
// Fragment shader header
`

func genWGSLFooter(numMaterials int, vec3Str string) string {
	wgslVec3 := strings.Replace(vec3Str, "fragVert", "fragVert", -1)

	switch numMaterials {
	default:
		return fmt.Sprintf(wgslFSFooterFmt4, wgslVec3)
	case 5, 6, 7, 8, 9:
		return fmt.Sprintf(wgslFSFooterFmt9, wgslVec3)
	case 10, 11, 12, 13, 14, 15, 16:
		return fmt.Sprintf(wgslFSFooterFmt16, wgslVec3)
	}
}

const wgslFSFooterFmt4 = `
@fragment
fn fs_main(@location(0) fragVert: vec3f) -> @location(0) vec4f {
    let u_slice = uniforms.u_slice;
    let u_materialNum = i32(uniforms.u_materialNum);
    let m = mainModel4(vec3f(%v));
    var color = 0.0;
    switch u_materialNum {
        case 1: { color = m.x; }
        case 2: { color = m.y; }
        case 3: { color = m.z; }
        case 4: { color = m.w; }
        default: { color = 0.0; }
    }
    return vec4f(color, color, color, 1.0);
}
`

const wgslFSFooterFmt9 = `
@fragment
fn fs_main(@location(0) fragVert: vec3f) -> @location(0) vec4f {
    let u_slice = uniforms.u_slice;
    let u_materialNum = i32(uniforms.u_materialNum);
    let m = mainModel9(vec3f(%v));
    var color = 0.0;
    switch u_materialNum {
        case 1: { color = m[0][0]; }
        case 2: { color = m[0][1]; }
        case 3: { color = m[0][2]; }
        case 4: { color = m[1][0]; }
        case 5: { color = m[1][1]; }
        case 6: { color = m[1][2]; }
        case 7: { color = m[2][0]; }
        case 8: { color = m[2][1]; }
        case 9: { color = m[2][2]; }
        default: { color = 0.0; }
    }
    return vec4f(color, color, color, 1.0);
}
`

const wgslFSFooterFmt16 = `
@fragment
fn fs_main(@location(0) fragVert: vec3f) -> @location(0) vec4f {
    let u_slice = uniforms.u_slice;
    let u_materialNum = i32(uniforms.u_materialNum);
    let m = mainModel16(vec3f(%v));
    var color = 0.0;
    switch u_materialNum {
        case 1: { color = m[0][0]; }
        case 2: { color = m[0][1]; }
        case 3: { color = m[0][2]; }
        case 4: { color = m[0][3]; }
        case 5: { color = m[1][0]; }
        case 6: { color = m[1][1]; }
        case 7: { color = m[1][2]; }
        case 8: { color = m[1][3]; }
        case 9: { color = m[2][0]; }
        case 10: { color = m[2][1]; }
        case 11: { color = m[2][2]; }
        case 12: { color = m[2][3]; }
        case 13: { color = m[3][0]; }
        case 14: { color = m[3][1]; }
        case 15: { color = m[3][2]; }
        case 16: { color = m[3][3]; }
        default: { color = 0.0; }
    }
    return vec4f(color, color, color, 1.0);
}
`
