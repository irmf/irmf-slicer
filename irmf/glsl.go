package irmf

import (
	"fmt"
	"image"
	"log"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
)

// OpenGLRenderer is a renderer implementation using OpenGL.
type OpenGLRenderer struct {
	window *glfw.Window
	width  int
	height int
	view   bool

	program             uint32
	vao                 uint32
	modelUniform        int32
	uMaterialNumUniform int32
	uSliceUniform       int32
}

func (r *OpenGLRenderer) Init(width, height int, view bool) error {
	if r.window != nil && (r.width != width || r.height != height) {
		glfw.Terminate()
		r.window = nil
	}
	r.width = width
	r.height = height
	r.view = view

	if r.window == nil {
		err := glfw.Init()
		if err != nil {
			return fmt.Errorf("glfw.Init: %v", err)
		}

		glfw.WindowHint(glfw.Resizable, glfw.False)
		glfw.WindowHint(glfw.ContextVersionMajor, 4)
		glfw.WindowHint(glfw.ContextVersionMinor, 1)
		glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
		glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
		if !r.view {
			glfw.WindowHint(glfw.Visible, glfw.False)
		}
		r.window, err = glfw.CreateWindow(width, height, "IRMF Slicer", nil, nil)
		if err != nil {
			return fmt.Errorf("CreateWindow(%v,%v): %v", width, height, err)
		}
		r.window.MakeContextCurrent()

		err = gl.Init()
		if err != nil {
			return fmt.Errorf("gl.Init: %v", err)
		}

		version := gl.GoStr(gl.GetString(gl.VERSION))
		log.Println("OpenGL version", version)
	}
	return nil
}

func (r *OpenGLRenderer) Prepare(irmf *IRMF, vec3Str string, planeVertices []float32, projection, camera, model mgl32.Mat4) error {
	// Configure the vertex and fragment shaders
	var err error
	if r.program, err = newProgram(vertexShader, fsHeader+irmf.Shader+genFooter(len(irmf.Materials), vec3Str)); err != nil {
		return fmt.Errorf("newProgram: %v", err)
	}

	gl.UseProgram(r.program)

	projectionUniform := gl.GetUniformLocation(r.program, gl.Str("projection\x00"))
	gl.UniformMatrix4fv(projectionUniform, 1, false, &projection[0])

	cameraUniform := gl.GetUniformLocation(r.program, gl.Str("camera\x00"))
	gl.UniformMatrix4fv(cameraUniform, 1, false, &camera[0])

	r.modelUniform = gl.GetUniformLocation(r.program, gl.Str("model\x00"))
	gl.UniformMatrix4fv(r.modelUniform, 1, false, &model[0])

	// Set up uniforms needed by shaders:
	uSlice := float32(0)
	r.uSliceUniform = gl.GetUniformLocation(r.program, gl.Str("u_slice\x00"))
	gl.Uniform1f(r.uSliceUniform, uSlice)
	uMaterialNum := int32(1)
	r.uMaterialNumUniform = gl.GetUniformLocation(r.program, gl.Str("u_materialNum\x00"))
	gl.Uniform1i(r.uMaterialNumUniform, uMaterialNum)

	gl.BindFragDataLocation(r.program, 0, gl.Str("outputColor\x00"))

	// Configure the vertex data
	gl.GenVertexArrays(1, &r.vao)
	gl.BindVertexArray(r.vao)

	var vbo uint32
	gl.GenBuffers(1, &vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(planeVertices)*4, gl.Ptr(planeVertices), gl.STATIC_DRAW)

	vertAttrib := uint32(gl.GetAttribLocation(r.program, gl.Str("vert\x00")))
	gl.EnableVertexAttribArray(vertAttrib)
	gl.VertexAttribPointer(vertAttrib, 3, gl.FLOAT, false, 5*4, gl.PtrOffset(0))

	// Configure global settings
	gl.Enable(gl.DEPTH_TEST)
	gl.DepthFunc(gl.LESS)
	gl.ClearColor(0.0, 0.0, 0.0, 0.0)

	return nil
}

func (r *OpenGLRenderer) Render(sliceDepth float32, materialNum int) (image.Image, error) {
	if e := gl.GetError(); e != gl.NO_ERROR {
		fmt.Printf("renderSlice, before gl.Clear: GL ERROR: %v\n", e)
	}

	gl.Clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT)

	// Render
	gl.UseProgram(r.program)
	// gl.UniformMatrix4fv(r.modelUniform, 1, false, &s.model[0]) // model is already set in Prepare
	gl.Uniform1f(r.uSliceUniform, float32(sliceDepth))
	gl.Uniform1i(r.uMaterialNumUniform, int32(materialNum))

	gl.BindVertexArray(r.vao)

	gl.DrawArrays(gl.TRIANGLES, 0, 2*3)

	if e := gl.GetError(); e != gl.NO_ERROR {
		fmt.Printf("renderSlice, after gl.DrawArrays: GL ERROR: %v\n", e)
	}

	width, height := r.window.GetFramebufferSize()
	rgba := &image.RGBA{
		Pix:    make([]uint8, width*height*4),
		Stride: width * 4,
		Rect:   image.Rect(0, 0, width, height),
	}
	gl.ReadPixels(0, 0, int32(width), int32(height), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(&rgba.Pix[0]))

	if e := gl.GetError(); e != gl.NO_ERROR {
		fmt.Printf("renderSlice, after gl.ReadPixels: GL ERROR: %v\n", e)
	}

	// Maintenance
	r.window.SwapBuffers()
	glfw.PollEvents()

	return rgba, nil
}

func (r *OpenGLRenderer) Close() {
	if r.window != nil {
		glfw.Terminate()
		r.window = nil
	}
}

func newProgram(vertexShaderSource, fragmentShaderSource string) (uint32, error) {
	vertexShader, err := compileShader(vertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}

	fragmentShader, err := compileShader(fragmentShaderSource, gl.FRAGMENT_SHADER)
	if err != nil {
		return 0, err
	}

	program := gl.CreateProgram()

	gl.AttachShader(program, vertexShader)
	gl.AttachShader(program, fragmentShader)
	gl.LinkProgram(program)

	var status int32
	gl.GetProgramiv(program, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetProgramiv(program, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetProgramInfoLog(program, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to link program: %v", log)
	}

	gl.DeleteShader(vertexShader)
	gl.DeleteShader(fragmentShader)

	return program, nil
}

func compileShader(source string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csources, free := gl.Strs(source)
	gl.ShaderSource(shader, 1, csources, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, fmt.Errorf("failed to compile %v: %v", source, log)
	}

	return shader, nil
}

const vertexShader = "#version 330\nuniform mat4 projection;\nuniform mat4 camera;\nuniform mat4 model;\nin vec3 vert;\nout vec3 fragVert;\nvoid main() {\n\tgl_Position = projection * camera * model * vec4(vert, 1);\n\tfragVert = vert;\n}"

const fsHeader = "#version 330\nprecision highp float;\nprecision highp int;\nin vec3 fragVert;\nout vec4 outputColor;\nuniform float u_slice;\nuniform int u_materialNum;"

func genFooter(numMaterials int, vec3Str string) string {
	switch numMaterials {
	default:
		return fmt.Sprintf(fsFooterFmt4, vec3Str) + "\x00"
	case 5, 6, 7, 8, 9:
		return fmt.Sprintf(fsFooterFmt9, vec3Str) + "\x00"
	case 10, 11, 12, 13, 14, 15, 16:
		return fmt.Sprintf(fsFooterFmt16, vec3Str) + "\x00"
	}
}

const fsFooterFmt4 = "\nvoid main() {\n  vec4 m;\n  mainModel4(m, vec3(%v));\n  switch(u_materialNum) {\n  case 1:\n    outputColor = vec4(m.x);\n    break;\n  case 2:\n    outputColor = vec4(m.y);\n    break;\n  case 3:\n    outputColor = vec4(m.z);\n    break;\n  case 4:\n    outputColor = vec4(m.w);\n    break;\n  }\n}"

const fsFooterFmt9 = "\nvoid main() {\n  mat3 m;\n  mainModel9(m, vec3(%v));\n  switch(u_materialNum) {\n  case 1:\n    outputColor = vec4(m[0][0]);\n    break;\n  case 2:\n    outputColor = vec4(m[0][1]);\n    break;\n  case 3:\n    outputColor = vec4(m[0][2]);\n    break;\n  case 4:\n    outputColor = vec4(m[1][0]);\n    break;\n  case 5:\n    outputColor = vec4(m[1][1]);\n    break;\n  case 6:\n    outputColor = vec4(m[1][2]);\n    break;\n  case 7:\n    outputColor = vec4(m[2][0]);\n    break;\n  case 8:\n    outputColor = vec4(m[2][1]);\n    break;\n  case 9:\n    outputColor = vec4(m[2][2]);\n    break;\n  }\n}"

const fsFooterFmt16 = "\nvoid main() {\n  mat4 m;\n  mainModel16(m, vec3(%v));\n  switch(u_materialNum) {\n  case 1:\n    outputColor = vec4(m[0][0]);\n    break;\n  case 2:\n    outputColor = vec4(m[0][1]);\n    break;\n  case 3:\n    outputColor = vec4(m[0][2]);\n    break;\n  case 4:\n    outputColor = vec4(m[0][3]);\n    break;\n  case 5:\n    outputColor = vec4(m[1][0]);\n    break;\n  case 6:\n    outputColor = vec4(m[1][1]);\n    break;\n  case 7:\n    outputColor = vec4(m[1][2]);\n    break;\n  case 8:\n    outputColor = vec4(m[1][3]);\n    break;\n  case 9:\n    outputColor = vec4(m[2][0]);\n    break;\n  case 10:\n    outputColor = vec4(m[2][1]);\n    break;\n  case 11:\n    outputColor = vec4(m[2][2]);\n    break;\n  case 12:\n    outputColor = vec4(m[2][3]);\n    break;\n  case 13:\n    outputColor = vec4(m[3][0]);\n    break;\n  case 14:\n    outputColor = vec4(m[3][1]);\n    break;\n  case 15:\n    outputColor = vec4(m[3][2]);\n    break;\n  case 16:\n    outputColor = vec4(m[3][3]);\n    break;\n  }\n}"
