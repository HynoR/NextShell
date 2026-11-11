//go:build windows

// Package d3d11 draws scenes with Direct3D 11 into a window through a DXGI
// flip model swap chain, called through syscall like the rest of the
// Windows backend (no cgo). Every op of a scene is an instanced quad drawn
// by a shader of its batch's classes (classes.hlsl) or the general one
// (shader.hlsl), compiled ahead of time into shaders.go;
// clips are scissor rectangles, with the innermost rounded clip computed
// in the shader. The swap chain is the window's, opaque, or with
// NewComposed shown on it through DirectComposition, with its alpha.
package d3d11

//go:generate go run gen.go

import (
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
	"weak"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/d3d11/device"
	"github.com/egoist/mygo/internal/scene"
)

var (
	dwmapi                          = syscall.NewLazyDLL(device.SystemDir() + `\dwmapi.dll`)
	procDwmFlush                    = dwmapi.NewProc("DwmFlush")
	procDwmGetCompositionTimingInfo = dwmapi.NewProc("DwmGetCompositionTimingInfo")
	user32                          = syscall.NewLazyDLL(device.SystemDir() + `\user32.dll`)
	procSetTimer                    = user32.NewProc("SetTimer")
	procKillTimer                   = user32.NewProc("KillTimer")
	procInvalidateRect              = user32.NewProc("InvalidateRect")
)

var procDCompositionCreateDevice = syscall.NewLazyDLL(device.SystemDir() + `\dcomp.dll`).NewProc("DCompositionCreateDevice")

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	iidIDXGIDevice     = guid{0x54ec77fa, 0x1377, 0x44e6, [8]byte{0x8c, 0x32, 0x88, 0xfd, 0x5f, 0x44, 0xc8, 0x4c}}
	iidIDXGIDevice1    = guid{0x77db970f, 0x6276, 0x48ba, [8]byte{0xba, 0x28, 0x07, 0x01, 0x43, 0xb4, 0x39, 0x2c}}
	iidIDXGIFactory2   = guid{0x50c83a1c, 0xe072, 0x4c48, [8]byte{0x87, 0xb0, 0x36, 0x30, 0xfa, 0x36, 0xa6, 0xd0}}
	iidIDXGISwapChain2 = guid{0xa8be2ac4, 0x199f, 0x4946, [8]byte{0xb3, 0x31, 0x79, 0x59, 0x9f, 0xb9, 0x8d, 0xe7}}
	iidID3D11Texture2D = guid{0x6f15aaf2, 0xd208, 0x4e89, [8]byte{0x9a, 0xb4, 0x48, 0x95, 0x35, 0xd3, 0x4f, 0x9c}}
	// ID3D11DeviceContext1 (Direct3D 11.1), whose ClearView clears a
	// rectangle.
	iidID3D11DeviceContext1 = guid{0xbb2c6faa, 0xb5fb, 0x4082, [8]byte{0x8e, 0x6b, 0x38, 0x8b, 0x8c, 0xfa, 0x90, 0xe1}}
)

var iidIDCompositionDevice = guid{0xc37ea93a, 0xe7aa, 0x450d, [8]byte{0xb1, 0x6f, 0x97, 0x46, 0xcb, 0x04, 0x07, 0xf3}}

// Vtable indices, from the Windows SDK headers.
const (
	release = 2

	// ID3D11Device
	devCreateBuffer             = 3
	devCreateTexture2D          = 5
	devCreateShaderResourceView = 7
	devCreateRenderTargetView   = 9
	devCreateInputLayout        = 11
	devCreateVertexShader       = 12
	devCreatePixelShader        = 15
	devCreateBlendState         = 20
	devCreateRasterizerState    = 22
	devCreateSamplerState       = 23
	devCreateQuery              = 24

	// ID3D11DeviceContext
	viewGetResource            = 7 // ID3D11View
	ctxVSSetConstantBuffers    = 7
	ctxPSSetShaderResources    = 8
	ctxPSSetShader             = 9
	ctxPSSetSamplers           = 10
	ctxVSSetShader             = 11
	ctxMap                     = 14
	ctxUnmap                   = 15
	ctxPSSetConstantBuffers    = 16
	ctxIASetInputLayout        = 17
	ctxIASetVertexBuffers      = 18
	ctxDrawInstanced           = 21
	ctxIASetPrimitiveTopology  = 24
	ctxBegin                   = 27
	ctxEnd                     = 28
	ctxGetData                 = 29
	ctxOMSetRenderTargets      = 33
	ctxOMSetBlendState         = 35
	ctxRSSetState              = 43
	ctxRSSetViewports          = 44
	ctxRSSetScissorRects       = 45
	ctxCopySubresourceRegion   = 46
	ctxUpdateSubresource       = 48
	ctxClearRenderTargetView   = 50
	ctxClearState              = 110
	ctxFlush                   = 111
	ctx1ClearView              = 132 // ID3D11DeviceContext1
	dxgiDevGetAdapter          = 7
	dxgiDevSetMaxLatency       = 12 // IDXGIDevice1::SetMaximumFrameLatency
	dxgiGetParent              = 6
	factoryMakeWindowAssoc     = 8
	factoryCreateSwapChainHwnd = 15
	factoryCreateSwapChainComp = 24 // CreateSwapChainForComposition
	scPresent                  = 8
	scPresent1                 = 22 // IDXGISwapChain1
	scSetBackgroundColor       = 25 // IDXGISwapChain1
	scGetBuffer                = 9
	scResizeBuffers            = 13
	scSetSourceSize            = 29 // IDXGISwapChain2
	scSetMaxLatency            = 31 // IDXGISwapChain2
	scLatencyWaitable          = 33 // IDXGISwapChain2::GetFrameLatencyWaitableObject

	// IDCompositionDevice, IDCompositionTarget and IDCompositionVisual
	dcompCommit           = 3
	dcompCreateTarget     = 6 // CreateTargetForHwnd
	dcompCreateVisual     = 7
	dcompTargetSetRoot    = 3
	dcompVisualSetContent = 15 // the overload taking an IUnknown
)

const (
	formatR32G32B32A32Float = 2
	formatR8G8B8A8Unorm     = 28
	formatB8G8R8A8Unorm     = 87
	formatR8Unorm           = 61

	usageDefault = 0
	usageDynamic = 2

	bindVertexBuffer   = 0x1
	bindConstantBuffer = 0x4
	bindShaderResource = 0x8
	bindRenderTarget   = 0x20

	cpuAccessWrite  = 0x10000
	mapWriteDiscard = 4

	topologyTriangleStrip = 5
)

// ptr turns an address from native code into a pointer.
func ptr(a uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&a)) }

// call calls method i of a COM object; the pointers passed as uintptr stay
// valid until it returns.
//
//go:uintptrescapes
func call(obj uintptr, i int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(ptr(obj))
	fn := *(*uintptr)(unsafe.Add(ptr(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	var a [16]uintptr
	a[0] = obj
	n := copy(a[1:], args)
	r, _, _ := syscall.SyscallN(fn, a[:n+1]...)
	return r
}

func failed(hr uintptr) bool { return int32(uint32(hr)) < 0 }

func free(obj *uintptr) {
	if *obj != 0 {
		call(*obj, release)
		*obj = 0
	}
}

type bufferDesc struct {
	ByteWidth, Usage, BindFlags, CPUAccessFlags, MiscFlags, StructureByteStride uint32
}

type subresourceData struct {
	SysMem                   uintptr
	SysMemPitch, SysMemSlice uint32
}

type texture2DDesc struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

type inputElement struct {
	SemanticName         *byte
	SemanticIndex        uint32
	Format               uint32
	InputSlot            uint32
	AlignedByteOffset    uint32
	InputSlotClass       uint32
	InstanceDataStepRate uint32
}

type rtBlend struct {
	BlendEnable                                 int32
	SrcBlend, DestBlend, BlendOp                uint32
	SrcBlendAlpha, DestBlendAlpha, BlendOpAlpha uint32
	WriteMask                                   uint8
	_                                           [3]byte
}

type blendDesc struct {
	AlphaToCoverage, IndependentBlend int32
	RenderTarget                      [8]rtBlend
}

type rasterizerDesc struct {
	FillMode, CullMode   uint32
	FrontCCW             int32
	DepthBias            int32
	DepthBiasClamp       float32
	SlopeScaledDepthBias float32
	DepthClip, Scissor   int32
	Multisample, AALines int32
}

type samplerDesc struct {
	Filter                       uint32
	AddressU, AddressV, AddressW uint32
	MipLODBias                   float32
	MaxAnisotropy                uint32
	ComparisonFunc               uint32
	BorderColor                  [4]float32
	MinLOD, MaxLOD               float32
}

type viewport struct{ X, Y, W, H, MinDepth, MaxDepth float32 }

type box struct{ Left, Top, Front, Right, Bottom, Back uint32 }

type mapped struct {
	Data                 uintptr
	RowPitch, DepthPitch uint32
}

// presentParameters is DXGI_PRESENT_PARAMETERS.
type presentParameters struct {
	DirtyRectsCount uint32
	DirtyRects      *gpu.Scissor // RECTs
	ScrollRect      uintptr
	ScrollOffset    uintptr
}

type swapChainDesc1 struct {
	Width, Height, Format          uint32
	Stereo                         int32
	SampleCount, SampleQuality     uint32
	BufferUsage, BufferCount       uint32
	Scaling, SwapEffect, AlphaMode uint32
	Flags                          uint32
}

type texture struct {
	tex, srv  uintptr
	rtv       uintptr // of a texture drawn into
	w, h      int
	gen, ver  uint64
	lastFrame uint64
}

// canvasTexture is the texture a canvas (scene.NewCanvas) is drawn into,
// in the back buffer's format, and what drawing it keeps, for as long as
// the canvas lives.
type canvasTexture struct {
	texture
	b      gpu.Builder
	canvas weak.Pointer[scene.Image]
}

// Renderer draws scenes into a window.
type Renderer struct {
	hwnd      uintptr
	device    uintptr
	ctx       uintptr
	factory   uintptr
	swapChain uintptr
	rtv       uintptr
	w, h      int  // the size drawn, which the swap chain shows
	warp      bool // the device is WARP, Windows' software rasterizer

	// composed tells that the swap chain shows on the window through
	// DirectComposition, with its alpha, as the content of visual, the
	// root of dcomp's target for the window.
	composed              bool
	dcomp, target, visual uintptr

	// swapChain2 is the swap chain's IDXGISwapChain2 (Windows 8.1), bw×bh
	// the size of its buffers, larger than w×h while resizing, and
	// resizedAt when the window last changed size. unscaled tells that the
	// window's swap chain shows its buffers unscaled (DXGI_SCALING_NONE).
	swapChain2 uintptr
	bw, bh     int
	unscaled   bool
	resizing   bool
	resizedAt  time.Time
	// bg is the background the window's swap chain shows beyond its
	// frame (setBackground), once bgSet.
	bg    scene.Color
	bgSet bool
	// lat is the swap chain's frame latency object (latency.go), pending
	// tells that the frame drawn presents once it is signaled, through
	// post, presentLater, and failed is why that failed; wake is what Ready
	// was given meanwhile, or while refreshing tells that a frame that
	// changed nothing waits for the display's next refresh (waitRefresh).
	lat          latency
	pending      bool
	refreshing   bool
	post         func(func())
	presentLater func()
	failed       error
	wake         func()
	// cost is what the last frames cost (latency.go), and wakeLater runs wake on
	// the main thread, once the screen has room for the frame Ready held back.
	cost      frameCost
	wakeLater func()

	vs, ps, layout      uintptr
	blend, raster, samp uintptr
	// holeBlend takes the coverage of holes (scene.OpHole) away.
	holeBlend uintptr
	// overBlend and overHoleBlend are blend and holeBlend for pixel shaders
	// writing one color (classes.hlsl), which blend by its alpha.
	overBlend, overHoleBlend uintptr
	// classes holds what the batches of each set of classes (gpu.Batch.Classes)
	// draw with, made as first drawn, and compileClasses that it compiles
	// classes.hlsl, as shaders.go is older.
	classes        [1 << gpu.NumClasses]*classPipe
	compileClasses bool
	cbuf           uintptr
	instBuf        uintptr
	instCap        int

	mask, color texture
	images      map[uint64]*texture
	frame       uint64
	swept       uint64 // the frame of the last sweep
	// canvases are the textures canvases are drawn into, and drawing the
	// render target drawn into now: the back buffer's, or a canvas's.
	canvases map[uint64]*canvasTexture
	drawing  uintptr

	// The passes computing the backdrops of effects: their shaders and
	// constants, grab, which holds the area of the frame read, and
	// backdrop, the textures they draw into, the second only along rows.
	passVS, downPS, blurPS, passCB uintptr
	grab                           texture
	backdrop                       [2]texture
	// effects are the pixel shaders of the effects drawn, made as first
	// drawn.
	effects map[*scene.Effect]*effectShader
	// shaderCB holds the uniforms of the shader being drawn (package
	// ui/shader), of shaderCBSize bytes, laid out in shaderBytes.
	shaderCB     uintptr
	shaderCBSize int
	shaderBytes  []byte
	// stops is the texture of the stops of gradients (register t10), and
	// stopsSRV the scene drawn's view of it, 0 for none; blurred are the
	// textures of the images the frame draws blurred, the first used of
	// them, and blurTmp the textures their passes go through.
	stops    texture
	stopsSRV uintptr
	blurred  []texture
	used     int
	blurTmp  [2]texture

	b gpu.Builder

	// Frames draw only what changed since the last one, as the swap chain
	// flips in sequence: DXGI then keeps what its buffers hold, and copies
	// into the one presented what the last frame changed outside the dirty
	// rectangle given (Present1). ctx1 is the context's
	// ID3D11DeviceContext1, whose ClearView clears the rectangle; diff
	// finds what changed, and pipelines what the ops draw with; held tells
	// that the swap chain shows the last frame drawn. part is the part of
	// the frame drawn, and redrew how much of it was, for FrameInfo.
	ctx1      uintptr
	diff      scene.Differ
	pipelines gpu.Pipelines
	held      bool
	part      gpu.Part
	redrew    redrawn
}

// redrawn is how much of a frame was drawn.
type redrawn int8

const (
	redrewWhole redrawn = iota
	redrewPart
	redrewNothing
)

// noPartial draws every frame whole, for measurements that compare.
var noPartial = os.Getenv("MYGO_PARTIAL") == "0"

// New creates a renderer drawing into the window hwnd, on the GPU or, when
// there is none, with Windows' software rasterizer (WARP). Its frames are
// opaque: what they leave transparent shows black.
func New(hwnd uintptr) (*Renderer, error) { return newRenderer(hwnd, false) }

// NewComposed creates a renderer whose frames show on the window hwnd
// through DirectComposition, premultiplied by their alpha, over what is
// behind the window's content: in a window without a redirection bitmap
// (WS_EX_NOREDIRECTIONBITMAP), its system backdrop, as Mica.
func NewComposed(hwnd uintptr) (*Renderer, error) { return newRenderer(hwnd, true) }

func newRenderer(hwnd uintptr, composed bool) (*Renderer, error) {
	if hwnd == 0 {
		return nil, errors.New("d3d11: no window")
	}
	r := &Renderer{hwnd: hwnd, composed: composed, images: map[uint64]*texture{}}
	var err error
	if r.device, r.ctx, r.warp, err = device.New(); err != nil {
		return nil, err
	}
	if err := r.init(); err != nil {
		r.Release()
		return nil, err
	}
	return r, nil
}

func (r *Renderer) init() error {
	// The factory that made the device's adapter makes the swap chain.
	var dxgiDev, adapter uintptr
	if failed(call(r.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice)), uintptr(unsafe.Pointer(&dxgiDev)))) {
		return errors.New("d3d11: no DXGI device")
	}
	defer free(&dxgiDev)
	// One frame queued ahead of the screen, not three: a frame shows the
	// input of when it was drawn while frames follow each other, as when
	// scrolling or animating.
	var dxgiDev1 uintptr
	if !failed(call(r.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice1)), uintptr(unsafe.Pointer(&dxgiDev1)))) {
		call(dxgiDev1, dxgiDevSetMaxLatency, 1)
		free(&dxgiDev1)
	}
	if noPartial || failed(call(r.ctx, 0, uintptr(unsafe.Pointer(&iidID3D11DeviceContext1)), uintptr(unsafe.Pointer(&r.ctx1)))) {
		r.ctx1 = 0
	}
	// Quads reach a pixel out of their shapes, glyphs half a pixel more, as
	// they draw at whole pixels.
	r.diff.Reach = 2
	if failed(call(dxgiDev, dxgiDevGetAdapter, uintptr(unsafe.Pointer(&adapter)))) {
		return errors.New("d3d11: no adapter")
	}
	defer free(&adapter)
	if failed(call(adapter, dxgiGetParent, uintptr(unsafe.Pointer(&iidIDXGIFactory2)), uintptr(unsafe.Pointer(&r.factory)))) {
		return errors.New("d3d11: DXGI 1.2 is required")
	}

	code, err := shaderCode()
	if err != nil {
		return err
	}
	r.compileClasses = sourceSum() != shaderSum || compileShaders
	vsCode := code.vs
	for _, s := range []struct {
		code []byte
		out  *uintptr
		vtbl int
		name string
	}{{code.vs, &r.vs, devCreateVertexShader, "vertex"}, {code.ps, &r.ps, devCreatePixelShader, "pixel"},
		{code.passVS, &r.passVS, devCreateVertexShader, "pass vertex"}, {code.down, &r.downPS, devCreatePixelShader, "down"},
		{code.blur, &r.blurPS, devCreatePixelShader, "blur"}} {
		if len(s.code) == 0 || failed(call(r.device, s.vtbl, uintptr(unsafe.Pointer(&s.code[0])), uintptr(len(s.code)), 0, uintptr(unsafe.Pointer(s.out)))) {
			return fmt.Errorf("d3d11: cannot create the %s shader", s.name)
		}
	}
	names := []string{"RECT", "RADII", "INNER", "COLOR", "COLOR", "COLOR", "GRAD", "UV", "CLIP", "CLIPR", "PARAMS", "TRANSFORM", "TRANSFORM", "TRANSFORM"}
	indices := []uint32{0, 0, 0, 0, 1, 2, 0, 0, 0, 0, 0, 0, 1, 2}
	elems := make([]inputElement, len(names))
	cstr := make([][]byte, len(names))
	for i, n := range names {
		cstr[i] = append([]byte(n), 0)
		elems[i] = inputElement{SemanticName: &cstr[i][0], SemanticIndex: indices[i], Format: formatR32G32B32A32Float,
			AlignedByteOffset: uint32(16 * i), InputSlotClass: 1, InstanceDataStepRate: 1}
	}
	if failed(call(r.device, devCreateInputLayout, uintptr(unsafe.Pointer(&elems[0])), uintptr(len(elems)),
		uintptr(unsafe.Pointer(&vsCode[0])), uintptr(len(vsCode)), uintptr(unsafe.Pointer(&r.layout)))) {
		return errors.New("d3d11: cannot create the input layout")
	}

	// Dual-source blending: the destination times one minus the shader's
	// second color, the source's alpha of each channel (D3D11_BLEND_ONE,
	// D3D11_BLEND_INV_SRC1_COLOR and D3D11_BLEND_INV_SRC1_ALPHA).
	bd := blendDesc{}
	bd.RenderTarget[0] = rtBlend{BlendEnable: 1, SrcBlend: 2, DestBlend: 17, BlendOp: 1, SrcBlendAlpha: 2, DestBlendAlpha: 19, BlendOpAlpha: 1, WriteMask: 0xf}
	if failed(call(r.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&r.blend)))) {
		return errors.New("d3d11: cannot create the blend state")
	}
	// Holes take none of the source (D3D11_BLEND_ZERO).
	bd.RenderTarget[0].SrcBlend, bd.RenderTarget[0].SrcBlendAlpha = 1, 1
	if failed(call(r.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&r.holeBlend)))) {
		return errors.New("d3d11: cannot create the blend state of holes")
	}
	// One color: its alpha is the source's (D3D11_BLEND_INV_SRC_ALPHA).
	bd.RenderTarget[0].SrcBlend, bd.RenderTarget[0].DestBlend, bd.RenderTarget[0].SrcBlendAlpha, bd.RenderTarget[0].DestBlendAlpha = 2, 6, 2, 6
	if failed(call(r.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&r.overBlend)))) {
		return errors.New("d3d11: cannot create the blend state of one color")
	}
	bd.RenderTarget[0].SrcBlend, bd.RenderTarget[0].SrcBlendAlpha = 1, 1
	if failed(call(r.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&r.overHoleBlend)))) {
		return errors.New("d3d11: cannot create the blend state of holes of one color")
	}
	r.b.Classes = true
	rd := rasterizerDesc{FillMode: 3, CullMode: 1, DepthClip: 1, Scissor: 1}
	if failed(call(r.device, devCreateRasterizerState, uintptr(unsafe.Pointer(&rd)), uintptr(unsafe.Pointer(&r.raster)))) {
		return errors.New("d3d11: cannot create the rasterizer state")
	}
	sd := samplerDesc{Filter: 0x15, AddressU: 3, AddressV: 3, AddressW: 3, ComparisonFunc: 1, MaxLOD: math.MaxFloat32}
	if failed(call(r.device, devCreateSamplerState, uintptr(unsafe.Pointer(&sd)), uintptr(unsafe.Pointer(&r.samp)))) {
		return errors.New("d3d11: cannot create the sampler")
	}
	cd := bufferDesc{ByteWidth: 16, Usage: usageDefault, BindFlags: bindConstantBuffer}
	if failed(call(r.device, devCreateBuffer, uintptr(unsafe.Pointer(&cd)), 0, uintptr(unsafe.Pointer(&r.cbuf)))) {
		return errors.New("d3d11: cannot create the constant buffer")
	}
	cd.ByteWidth = uint32(unsafe.Sizeof(gpu.Pass{}))
	if failed(call(r.device, devCreateBuffer, uintptr(unsafe.Pointer(&cd)), 0, uintptr(unsafe.Pointer(&r.passCB)))) {
		return errors.New("d3d11: cannot create the constant buffer of passes")
	}
	return nil
}

// Resizing the buffers of a swap chain, as each step of a resize would,
// takes DWM longer than a frame for a window about as large as a 4K
// screen. So while the window changes size, the swap chain has three
// buffers, a quarter larger than the window, and shows the window's size
// of them (SetSourceSize): they are resized only when the window outgrows
// them, and a frame is free to draw while DWM holds two. A frame
// settleDelay after the last change, which the settle timer asks for,
// gives the swap chain two buffers of the window's size again. A swap chain
// that does not stretch its frames over the window, for composition or
// unscaled (#855), stretches the size it shows over its buffers' instead,
// which would magnify the frames (#892): it shows its buffers whole, and
// the window clips what is beyond the frame, which the frame leaves
// transparent, or fills with the content's background for an unscaled
// window's swap chain (clearWhole), as it shows where the window grows
// before the frame of its new size presents.
const (
	resizeBuffers = 3
	settleTimer   = 0x6d79 // the timer's ID on the window
)

var settleDelay = time.Second

// resize makes the swap chain show w×h pixels.
func (r *Renderer) resize(w, h int) error {
	if r.swapChain != 0 && w == r.w && h == r.h {
		if r.resizing && time.Since(r.resizedAt) >= settleDelay/2 {
			r.resizing = false
			return r.buffers(w, h, w, h, 2)
		}
		return nil
	}
	if r.swapChain == 0 || r.swapChain2 == 0 {
		return r.buffers(w, h, w, h, 2)
	}
	r.resizedAt = time.Now()
	r.armSettle()
	if r.resizing && w <= r.bw && h <= r.bh {
		if err := r.setSourceSize(r.shown(w, h, r.bw, r.bh)); err != nil {
			return err
		}
		r.w, r.h = w, h
		return nil
	}
	r.resizing = true
	return r.buffers(w, h, w+w/4+64, h+h/4+64, resizeBuffers)
}

// buffers makes the swap chain n buffers of bw×bh pixels, which shows w×h
// of them.
func (r *Renderer) buffers(w, h, bw, bh, n int) error {
	free(&r.rtv)
	r.held = false
	if r.swapChain == 0 {
		desc := swapChainDesc1{Width: uint32(bw), Height: uint32(bh), Format: formatB8G8R8A8Unorm, SampleCount: 1,
			BufferUsage: 0x20, BufferCount: uint32(n), Scaling: scalingStretch, SwapEffect: 3 /* flip sequential */, Flags: swapChainWaitable}
		if r.composed {
			if err := r.composeSwapChain(desc); err != nil {
				return err
			}
		} else {
			// The window's swap chain shows its frame unscaled, from the
			// top left corner, and its background beyond (setBackground):
			// where the window grew before the frame of its new size
			// presents, DWM shows the content's background, not black,
			// nor the last frame stretched.
			desc.Scaling = scalingNone
			hr := call(r.factory, factoryCreateSwapChainHwnd, r.device, r.hwnd, uintptr(unsafe.Pointer(&desc)), 0, 0, uintptr(unsafe.Pointer(&r.swapChain)))
			r.unscaled = !failed(hr)
			if failed(hr) {
				desc.Scaling = scalingStretch
				hr = call(r.factory, factoryCreateSwapChainHwnd, r.device, r.hwnd, uintptr(unsafe.Pointer(&desc)), 0, 0, uintptr(unsafe.Pointer(&r.swapChain)))
			}
			if failed(hr) {
				return fmt.Errorf("d3d11: cannot create the swap chain: %#x", uint32(hr))
			}
			const noAltEnter = 2
			call(r.factory, factoryMakeWindowAssoc, r.hwnd, noAltEnter)
		}
		call(r.swapChain, 0, uintptr(unsafe.Pointer(&iidIDXGISwapChain2)), uintptr(unsafe.Pointer(&r.swapChain2)))
		if r.swapChain2 != 0 {
			// One frame waits ahead of the screen, which the frame
			// latency object tells room for.
			call(r.swapChain2, scSetMaxLatency, 1)
			r.lat.handle = call(r.swapChain2, scLatencyWaitable)
		}
	} else if hr := call(r.swapChain, scResizeBuffers, uintptr(n), uintptr(bw), uintptr(bh), 0, swapChainWaitable); failed(hr) {
		return fmt.Errorf("d3d11: cannot resize the swap chain: %#x", uint32(hr))
	}
	if err := r.setSourceSize(r.shown(w, h, bw, bh)); err != nil {
		return err
	}
	r.bw, r.bh = bw, bh
	var back uintptr
	if hr := call(r.swapChain, scGetBuffer, 0, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&back))); failed(hr) {
		return fmt.Errorf("d3d11: no back buffer: %#x", uint32(hr))
	}
	defer free(&back)
	if hr := call(r.device, devCreateRenderTargetView, back, 0, uintptr(unsafe.Pointer(&r.rtv))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create the render target view: %#x", uint32(hr))
	}
	r.w, r.h = w, h
	return nil
}

// DXGI_SCALING: how a window's swap chain shows a frame of another size
// than the window.
const (
	scalingStretch = 0
	scalingNone    = 1
)

// setBackground makes the window's swap chain show c beyond its buffers
// (IDXGISwapChain1::SetBackgroundColor), where the window is larger than
// them, as it grows until the frame of its size presents: the content's
// background (scene.Scene.Background), opaque, or black without one. A swap
// chain for composition has none. It costs a comparison but when c
// changes, and reports whether it did.
func (r *Renderer) setBackground(c scene.Color) bool {
	if r.composed || r.swapChain == 0 || r.bgSet && c == r.bg {
		return false
	}
	r.bg, r.bgSet = c, true
	rgba := r.background()
	call(r.swapChain, scSetBackgroundColor, uintptr(unsafe.Pointer(&rgba)))
	return true
}

// background is the content's background (setBackground) over black, as
// the window's swap chain shows it.
func (r *Renderer) background() [4]float32 {
	rgba := r.bg.Premul(1)
	rgba[3] = 1
	return rgba
}

// shown is the size of the part at the top left of bw×bh buffers that the
// swap chain shows for a frame of w×h: the frame, which a window's swap
// chain that stretches stretches over the window, or the buffers whole when
// the swap chain does not stretch its frames over the window, as one for
// composition, or an unscaled one, which would stretch the part over the
// buffers' size, magnifying the frame (#892).
func (r *Renderer) shown(w, h, bw, bh int) (int, int) {
	if r.composed || r.unscaled {
		return bw, bh
	}
	return w, h
}

// beyondFrame returns the parts of bw×bh buffers beyond a frame of w×h at
// their top left, n of them: the strip at its right, as tall as the
// buffers, and the one under it. None is when the frame fills the buffers.
func beyondFrame(w, h, bw, bh int) (parts [2]gpu.Scissor, n int) {
	if w < bw {
		parts[n] = gpu.Scissor{Left: int32(w), Right: int32(bw), Bottom: int32(bh)}
		n++
	}
	if h < bh {
		parts[n] = gpu.Scissor{Top: int32(h), Right: int32(min(w, bw)), Bottom: int32(bh)}
		n++
	}
	return parts, n
}

// clearWhole clears rtv for a frame drawn whole, with clear. What the
// window's unscaled swap chain shows of its buffers beyond the frame, where
// the window grows before the frame of its new size presents, it fills
// with the content's opaque background: not black, nor what an earlier,
// larger frame left there. Without ClearView, it clears the frame with the
// background too, which the content fills it with anyway.
func (r *Renderer) clearWhole(rtv uintptr, s *scene.Scene, clear [4]float32) {
	beyond := rtv == r.rtv && r.unscaled && r.bg.A == 255 && (s.Width < r.bw || s.Height < r.bh)
	bg := r.background()
	if beyond && r.ctx1 == 0 {
		clear = bg
	}
	call(r.ctx, ctxClearRenderTargetView, rtv, uintptr(unsafe.Pointer(&clear[0])))
	if beyond && r.ctx1 != 0 {
		parts, n := beyondFrame(s.Width, s.Height, r.bw, r.bh)
		call(r.ctx1, ctx1ClearView, rtv, uintptr(unsafe.Pointer(&bg[0])), uintptr(unsafe.Pointer(&parts[0])), uintptr(n))
	}
}

// setSourceSize makes the swap chain show the w×h pixels at the top left
// of its buffers (shown), unless it is for composition, which shows them
// whole.
func (r *Renderer) setSourceSize(w, h int) error {
	if r.swapChain2 == 0 || r.composed {
		return nil
	}
	if hr := call(r.swapChain2, scSetSourceSize, uintptr(w), uintptr(h)); failed(hr) {
		return fmt.Errorf("d3d11: cannot set the swap chain's source size: %#x", uint32(hr))
	}
	return nil
}

// composeSwapChain creates a swap chain for DirectComposition, whose
// pixels are premultiplied by their alpha, and shows it on the window: the
// content of the visual at the root of a target for the window, on a
// DirectComposition device of the renderer's own. The swap chain's frames
// show as it presents them, with no commit.
func (r *Renderer) composeSwapChain(desc swapChainDesc1) error {
	if err := procDCompositionCreateDevice.Find(); err != nil {
		return fmt.Errorf("d3d11: no DirectComposition: %w", err)
	}
	var dxgiDev uintptr
	if failed(call(r.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice)), uintptr(unsafe.Pointer(&dxgiDev)))) {
		return errors.New("d3d11: no DXGI device")
	}
	hr, _, _ := procDCompositionCreateDevice.Call(dxgiDev, uintptr(unsafe.Pointer(&iidIDCompositionDevice)), uintptr(unsafe.Pointer(&r.dcomp)))
	free(&dxgiDev)
	if failed(hr) {
		return fmt.Errorf("d3d11: cannot create the DirectComposition device: %#x", uint32(hr))
	}
	// A swap chain for composition flips in sequence and stretches, the
	// only ways it takes.
	desc.SwapEffect, desc.AlphaMode = 3 /* flip sequential */, 1 /* premultiplied */
	if hr := call(r.factory, factoryCreateSwapChainComp, r.device, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&r.swapChain))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create the swap chain for composition: %#x", uint32(hr))
	}
	if hr := call(r.dcomp, dcompCreateTarget, r.hwnd, 1, uintptr(unsafe.Pointer(&r.target))); failed(hr) {
		return fmt.Errorf("d3d11: cannot compose the window: %#x", uint32(hr))
	}
	if hr := call(r.dcomp, dcompCreateVisual, uintptr(unsafe.Pointer(&r.visual))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create the visual: %#x", uint32(hr))
	}
	if hr := call(r.visual, dcompVisualSetContent, r.swapChain); failed(hr) {
		return fmt.Errorf("d3d11: cannot show the swap chain in the visual: %#x", uint32(hr))
	}
	if hr := call(r.target, dcompTargetSetRoot, r.visual); failed(hr) {
		return fmt.Errorf("d3d11: cannot show the visual on the window: %#x", uint32(hr))
	}
	if hr := call(r.dcomp, dcompCommit); failed(hr) {
		return fmt.Errorf("d3d11: cannot commit the composition: %#x", uint32(hr))
	}
	return nil
}

// Composed reports whether the frames show through DirectComposition, with
// their alpha (NewComposed).
func (r *Renderer) Composed() bool { return r.composed }

// Software reports whether the renderer draws with WARP, on the CPU, for
// want of a GPU.
func (r *Renderer) Software() bool { return r.warp }

// settling holds the windows whose settle timer is armed; settleProc is
// the timers' callback, made once.
var (
	settling   = map[uintptr]bool{}
	settleProc uintptr
)

// armSettle has the window ask for a frame settleDelay from now, unless
// it changes size again before.
func (r *Renderer) armSettle() {
	if settleProc == 0 {
		settleProc = syscall.NewCallback(func(hwnd, msg, id, ms uintptr) uintptr {
			procKillTimer.Call(hwnd, id)
			if settling[hwnd] {
				delete(settling, hwnd)
				procInvalidateRect.Call(hwnd, 0, 0)
			}
			return 0
		})
	}
	settling[r.hwnd] = true
	procSetTimer.Call(r.hwnd, settleTimer, uintptr(settleDelay/time.Millisecond), settleProc)
}

// Release frees the renderer's GPU objects.
func (r *Renderer) Release() {
	r.pending, r.refreshing, r.wake = false, false, nil
	r.lat.release()
	r.cost.release()
	r.b.Release()
	if settling[r.hwnd] {
		delete(settling, r.hwnd)
		procKillTimer.Call(r.hwnd, settleTimer)
	}
	if r.ctx != 0 {
		call(r.ctx, ctxClearState)
		call(r.ctx, ctxFlush)
	}
	for _, t := range r.images {
		free(&t.srv)
		free(&t.tex)
	}
	r.images = nil
	for _, t := range r.canvases {
		freeCanvas(t)
	}
	r.canvases = nil
	for _, t := range []*texture{&r.mask, &r.color} {
		free(&t.srv)
		free(&t.tex)
	}
	for _, p := range r.effects {
		free(&p.ps)
	}
	for _, s := range classShaders {
		if p := r.classes[s.classes]; p != nil {
			free(&p.vs)
			free(&p.ps)
		}
	}
	free(&r.shaderCB)
	for i := range r.blurred {
		t := &r.blurred[i]
		free(&t.rtv)
		free(&t.srv)
		free(&t.tex)
	}
	r.blurred = nil
	for _, t := range []*texture{&r.grab, &r.backdrop[0], &r.backdrop[1], &r.blurTmp[0], &r.blurTmp[1], &r.stops} {
		free(&t.rtv)
		free(&t.srv)
		free(&t.tex)
	}
	// The target goes first: a window has one at most, which another
	// renderer of it then makes.
	for _, p := range []*uintptr{&r.visual, &r.target, &r.dcomp, &r.rtv, &r.swapChain2, &r.swapChain, &r.instBuf, &r.cbuf, &r.passCB, &r.samp, &r.raster, &r.overHoleBlend, &r.overBlend, &r.holeBlend, &r.blend, &r.layout,
		&r.blurPS, &r.downPS, &r.passVS, &r.ps, &r.vs, &r.factory, &r.ctx1, &r.ctx, &r.device} {
		free(p)
	}
}

// newTexture creates a texture of w×h pixels from pix.
func (r *Renderer) newTexture(t *texture, w, h int, format uint32, pix []byte, bpp int) error {
	free(&t.srv)
	free(&t.tex)
	desc := texture2DDesc{Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1, Format: format, SampleCount: 1, Usage: usageDefault, BindFlags: bindShaderResource}
	var init *subresourceData
	if len(pix) >= w*h*bpp {
		init = &subresourceData{SysMem: uintptr(unsafe.Pointer(&pix[0])), SysMemPitch: uint32(w * bpp)}
	}
	if hr := call(r.device, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), uintptr(unsafe.Pointer(init)), uintptr(unsafe.Pointer(&t.tex))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create a %dx%d texture: %#x", w, h, uint32(hr))
	}
	if hr := call(r.device, devCreateShaderResourceView, t.tex, 0, uintptr(unsafe.Pointer(&t.srv))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create a texture view: %#x", uint32(hr))
	}
	t.w, t.h = w, h
	return nil
}

// syncAtlas uploads what changed in an atlas.
func (r *Renderer) syncAtlas(t *texture, a *scene.Atlas, format uint32) error {
	if a == nil {
		return nil
	}
	if t.tex == 0 || t.gen != a.Generation() || t.w != a.W || t.h != a.H {
		if err := r.newTexture(t, a.W, a.H, format, a.Pix, a.BPP); err != nil {
			return err
		}
		t.gen, t.ver = a.Generation(), a.Version()
		return nil
	}
	rects, full := a.Changes(t.gen, t.ver)
	if full {
		call(r.ctx, ctxUpdateSubresource, t.tex, 0, 0, uintptr(unsafe.Pointer(&a.Pix[0])), uintptr(a.W*a.BPP), 0)
	}
	for _, rc := range rects {
		b := box{Left: uint32(rc.Min.X), Top: uint32(rc.Min.Y), Front: 0, Right: uint32(rc.Max.X), Bottom: uint32(rc.Max.Y), Back: 1}
		src := a.Pix[(rc.Min.Y*a.W+rc.Min.X)*a.BPP:]
		call(r.ctx, ctxUpdateSubresource, t.tex, 0, uintptr(unsafe.Pointer(&b)), uintptr(unsafe.Pointer(&src[0])), uintptr(a.W*a.BPP), 0)
	}
	t.ver = a.Version()
	return nil
}

// imageView returns the texture view of an image, uploading it when new or
// changed.
func (r *Renderer) imageView(img *scene.Image) uintptr {
	if img.Scene != nil {
		// A canvas, drawn before the scene showing it.
		if c := r.canvases[img.ID()]; c != nil {
			return c.srv
		}
		return 0
	}
	t := r.images[img.ID()]
	if t == nil {
		pix := img.Pixels()
		if pix == nil {
			return 0
		}
		t = &texture{}
		if err := r.newTexture(t, img.W, img.H, formatR8G8B8A8Unorm, pix, 4); err != nil {
			return 0
		}
		t.ver = img.Version()
		r.images[img.ID()] = t
		img.Uploaded()
	} else if t.w != img.W || t.h != img.H {
		if pix := img.Pixels(); pix != nil {
			if err := r.newTexture(t, img.W, img.H, formatR8G8B8A8Unorm, pix, 4); err != nil {
				return 0
			}
			t.ver = img.Version()
			img.Uploaded()
		}
	} else if t.ver != img.Version() {
		if pix := img.Pixels(); pix != nil {
			call(r.ctx, ctxUpdateSubresource, t.tex, 0, 0, uintptr(unsafe.Pointer(&pix[0])), uintptr(img.W*4), 0)
			t.ver = img.Version()
			img.Uploaded()
		}
	}
	t.lastFrame = r.frame
	return t.srv
}

// Render draws s into the window and presents it.
func (r *Renderer) Render(s *scene.Scene) error {
	if s.Width <= 0 || s.Height <= 0 {
		return nil
	}
	if r.pending {
		// A frame not paced by the display, as a resize's, draws after
		// the one waiting to present.
		r.presentPending()
	}
	if err := r.failed; err != nil {
		r.failed = nil
		return err
	}
	resized := r.swapChain != 0 && (s.Width != r.w || s.Height != r.h)
	drawn, err := r.drawFrame(s)
	if err != nil {
		return err
	}
	if !drawn {
		r.waitRefresh()
		return nil
	}
	// A frame of a new size is presented at once (sync interval 0), not
	// after the frame queued before it, which would miss the next
	// composition, and waits until the compositor shows it: the window
	// takes its next size, as the user drags its border, with the frame of
	// that size, rather than show older frames stretched to it.
	sync := uintptr(1)
	if resized {
		sync = 0
	} else if r.presentLater != nil {
		r.cost.read(r.ctx)
		if r.cost.fits() && r.lat.ready() {
			// It shows at the next composition, and the next frame builds
			// after that (latency.go).
			if err := r.present(0); err != nil {
				return err
			}
			r.waitComposed()
			return nil
		}
		if !r.lat.ready() {
			// The screen has no room for the frame yet: it presents once it
			// has, while the main thread does something else, and the GPU
			// draws it.
			call(r.ctx, ctxFlush)
			r.pending = true
			r.lat.waitThen(r.presentLater)
			return nil
		}
	}
	if err := r.present(sync); err != nil {
		return err
	}
	if resized {
		procDwmFlush.Call()
	}
	return nil
}

// draw draws s whole into the swap chain's back buffer, without
// presenting it, for tests and RenderOffscreen.
func (r *Renderer) draw(s *scene.Scene) error {
	r.held = false
	_, err := r.drawFrame(s)
	return err
}

// drawFrame draws s into the swap chain's back buffer, only where it
// changed since the last frame when the swap chain shows that one, and
// reports whether it drew: a frame that changed nothing draws nothing.
func (r *Renderer) drawFrame(s *scene.Scene) (bool, error) {
	r.frame++
	r.used = 0
	if err := r.resize(s.Width, s.Height); err != nil {
		return false, err
	}
	if r.setBackground(s.Background) {
		// What the buffers hold beyond the frame takes the new background
		// (clearWhole).
		r.held = false
	}
	var damage []image.Rectangle
	known := false
	if r.ctx1 != 0 {
		damage, known = r.diff.Diff(s)
		known = known && r.held
		if known && len(damage) == 0 {
			// The swap chain shows the frame already.
			r.redrew = redrewNothing
			return false, nil
		}
	}
	if err := r.syncAtlas(&r.mask, s.MaskAtlas, formatR8Unorm); err != nil {
		return false, err
	}
	if err := r.syncAtlas(&r.color, s.ColorAtlas, formatR8G8B8A8Unorm); err != nil {
		return false, err
	}
	if err := r.drawCanvases(s); err != nil {
		return false, err
	}
	r.drawing = r.rtv
	built, err := r.buildScene(s, &r.b)
	if err != nil {
		return false, err
	}
	var part *gpu.Part
	r.redrew = redrewWhole
	if r.ctx1 != 0 {
		damage = r.pipelines.Damage(&r.b, r.batchKey, &r.diff, damage, known)
		if known && r.part.Set(damage, s.Width, s.Height, func(d []image.Rectangle) []image.Rectangle { return r.diff.AddBackdrops(s, d) }) {
			if len(r.part.Rects) == 0 {
				// What changed is outside the frame: the swap chain shows
				// it already.
				r.redrew = redrewNothing
				r.diff.Remember(s)
				r.pipelines.Remember()
				return false, nil
			}
			part, r.redrew = &r.part, redrewPart
		}
	}
	r.held = false
	if err := r.drawBuilt(built, &r.b, r.rtv, part); err != nil {
		return false, err
	}
	if r.ctx1 != 0 {
		r.diff.Remember(s)
		r.pipelines.Remember()
	}
	return true, nil
}

// batchKey returns what batch i of the frame built draws with: its pixel
// shader and blending (gpu.Pipelines).
func (r *Renderer) batchKey(i int) uint64 {
	bt := &r.b.Batches[i]
	_, ps, _ := r.pipeline(bt)
	key := uint64(ps) << 1
	if bt.Hole {
		key |= 1
	}
	return key
}

// drawCanvases draws the canvases s draws anew or shows into their
// textures, unless they hold their versions already.
func (r *Renderer) drawCanvases(s *scene.Scene) (err error) {
	s.EachCanvas(func(c *scene.Image) {
		if err == nil {
			err = r.drawCanvas(c)
		}
	})
	return err
}

func (r *Renderer) drawCanvas(c *scene.Image) error {
	if r.canvases == nil {
		r.canvases = map[uint64]*canvasTexture{}
	}
	t := r.canvases[c.ID()]
	if t == nil {
		t = &canvasTexture{canvas: weak.Make(c)}
		t.b.Classes, t.b.NoClasses = r.b.Classes, r.b.NoClasses
		r.canvases[c.ID()] = t
	}
	t.lastFrame = r.frame // drawn or shown: the sweep keeps a transient canvas's texture
	if t.tex != 0 && t.w == c.W && t.h == c.H && t.ver == c.Version() {
		return nil
	}
	if t.tex == 0 || t.w != c.W || t.h != c.H {
		if err := r.newTarget(&t.texture, c.W, c.H, true); err != nil {
			return err
		}
		t.lastFrame = r.frame // newTarget clears the texture's state
	}
	cs := c.Scene
	cs.Width, cs.Height = c.W, c.H
	if err := r.drawScene(cs, &t.b, t.rtv); err != nil {
		return err
	}
	t.ver = c.Version()
	return nil
}

func freeCanvas(t *canvasTexture) {
	free(&t.rtv)
	free(&t.srv)
	free(&t.tex)
	t.b.Release()
}

// drawScene draws s into the render target rtv with b.
func (r *Renderer) drawScene(s *scene.Scene, b *gpu.Builder, rtv uintptr) error {
	r.drawing = rtv
	built, err := r.buildScene(s, b)
	if err != nil {
		return err
	}
	return r.drawBuilt(built, b, rtv, nil)
}

// buildScene builds the instances of s with b, with the textures they
// read, for the render target r.drawing, and returns the scene built.
func (r *Renderer) buildScene(s *scene.Scene, b *gpu.Builder) (*scene.Scene, error) {
	b.BlurredImage = r.blurImage
	s = b.Build(s, r.imageView)
	b.BlurredImage = nil
	if err := r.syncStops(b); err != nil {
		return nil, err
	}
	if err := r.fitBackdrop(b); err != nil {
		return nil, err
	}
	return s, nil
}

// drawBuilt draws s, built with b (buildScene), into the render target
// rtv, whole or, unless part is nil, only within its rectangles, keeping
// what rtv holds elsewhere.
func (r *Renderer) drawBuilt(s *scene.Scene, b *gpu.Builder, rtv uintptr, part *gpu.Part) error {
	r.drawing = rtv
	if part != nil && len(part.Rects) == 0 {
		return nil
	}

	// Upload the instances, growing the buffer as needed.
	if n := len(b.Instances); n > 0 {
		if n > r.instCap {
			free(&r.instBuf)
			capacity := max(n*3/2, 1024)
			bd := bufferDesc{ByteWidth: uint32(capacity * gpu.InstanceSize), Usage: usageDynamic, BindFlags: bindVertexBuffer, CPUAccessFlags: cpuAccessWrite}
			if hr := call(r.device, devCreateBuffer, uintptr(unsafe.Pointer(&bd)), 0, uintptr(unsafe.Pointer(&r.instBuf))); failed(hr) {
				r.instCap = 0
				return fmt.Errorf("d3d11: cannot create the instance buffer: %#x", uint32(hr))
			}
			r.instCap = capacity
		}
		var m mapped
		if hr := call(r.ctx, ctxMap, r.instBuf, 0, mapWriteDiscard, 0, uintptr(unsafe.Pointer(&m))); failed(hr) {
			return fmt.Errorf("d3d11: cannot map the instance buffer: %#x", uint32(hr))
		}
		dst := unsafe.Slice((*gpu.Instance)(ptr(m.Data)), n)
		copy(dst, b.Instances)
		call(r.ctx, ctxUnmap, r.instBuf, 0)
	}
	globals := [4]float32{float32(s.Width), float32(s.Height), 0, 0}
	call(r.ctx, ctxUpdateSubresource, r.cbuf, 0, 0, uintptr(unsafe.Pointer(&globals[0])), 0, 0)

	ctx := r.ctx
	call(ctx, ctxOMSetRenderTargets, 1, uintptr(unsafe.Pointer(&rtv)), 0)
	clear := s.Clear.Premul(1)
	areas := []gpu.Scissor{{Right: int32(s.Width), Bottom: int32(s.Height)}}
	if part != nil {
		areas = part.Rects
		call(r.ctx1, ctx1ClearView, rtv, uintptr(unsafe.Pointer(&clear[0])), uintptr(unsafe.Pointer(&areas[0])), uintptr(len(areas)))
	} else {
		r.clearWhole(rtv, s, clear)
	}
	if rtv == r.rtv {
		if q := r.cost.begin(ctx); q != nil {
			defer r.cost.end(ctx, q)
		}
	}
	call(ctx, ctxRSSetState, r.raster)
	call(ctx, ctxIASetPrimitiveTopology, topologyTriangleStrip)
	r.bindState(s, 0)
	bound, vs, ps, blend, shaders := uintptr(0), r.vs, r.ps, r.blend, false
	mask, scissor := ^uintptr(0), gpu.Scissor{} // what no batch has, to bind the first's
	for i := range b.Batches {
		bt := &b.Batches[i]
		if bt.Count == 0 || !meets(bt.Scissor, areas) {
			continue
		}
		wantVS, want, wantBlend := r.pipeline(bt)
		if want == 0 {
			continue
		}
		if bt.Backdrop != 0 {
			// The effect shows what is drawn so far.
			r.readBackdrop(b.Backdrops[bt.Backdrop-1])
			r.bindState(s, r.backdrop[0].srv)
			bound, vs, ps, blend, mask, scissor = 0, r.vs, r.ps, r.blend, ^uintptr(0), gpu.Scissor{}
		}
		if wantVS != vs {
			vs = wantVS
			call(ctx, ctxVSSetShader, vs, 0, 0)
		}
		if want != ps {
			ps = want
			call(ctx, ctxPSSetShader, ps, 0, 0)
		}
		if wantBlend != blend {
			blend = wantBlend
			call(ctx, ctxOMSetBlendState, blend, 0, 0xffffffff)
		}
		if bt.Image != 0 && bt.Image != bound {
			bound = bt.Image
			call(ctx, ctxPSSetShaderResources, 2, 1, uintptr(unsafe.Pointer(&bound)))
		}
		if bt.ClipMask != mask {
			mask = bt.ClipMask
			call(ctx, ctxPSSetShaderResources, 5, 1, uintptr(unsafe.Pointer(&mask)))
		}
		if bt.Shader != nil {
			if err := r.bindShader(*bt); err != nil {
				return err
			}
			shaders = true
		}
		// Drawn once in each rectangle of the part it meets, which are
		// apart from each other.
		for _, a := range areas {
			sc := bt.Scissor.Intersect(a) // a D3D11_RECT
			if sc.Empty() {
				continue
			}
			if sc != scissor {
				scissor = sc
				call(ctx, ctxRSSetScissorRects, 1, uintptr(unsafe.Pointer(&sc)))
			}
			call(ctx, ctxDrawInstanced, 4, uintptr(bt.Count), 0, uintptr(bt.Start))
		}
	}
	if shaders {
		// Images may be drawn into next, as canvases.
		var none [4]uintptr
		call(ctx, ctxPSSetShaderResources, 6, 4, uintptr(unsafe.Pointer(&none[0])))
	}
	return nil
}

// classPipe is what the batches of some classes draw with: its shaders, 0
// when they could not be made, as then the general shader draws them.
type classPipe struct {
	vs, ps uintptr
	// dual tells that its pixel shader writes a second color, for dual-source
	// blending.
	dual bool
}

// pipeline returns the shaders and the blending batch bt draws with: its
// classes', the general shader's, or its effect's, whose pixel shader is 0 when
// it does not compile.
func (r *Renderer) pipeline(bt *gpu.Batch) (vs, ps, blend uintptr) {
	vs, ps, blend = r.vs, r.ps, r.blend
	if bt.Hole {
		blend = r.holeBlend
	}
	if bt.Effect != nil {
		return vs, r.effectShader(bt.Effect), blend
	}
	cl := bt.Classes()
	if cl&(1<<gpu.ClassGeneral) != 0 || cl >= 1<<gpu.NumClasses {
		return vs, ps, blend
	}
	p := r.classes[cl]
	if p == nil {
		p = r.classPipeFor(cl)
	}
	if p.ps == 0 {
		return vs, ps, blend
	}
	if !p.dual {
		blend = r.overBlend
		if bt.Hole {
			blend = r.overHoleBlend
		}
	}
	return p.vs, p.ps, blend
}

// classPipeFor returns the shaders drawing the classes cl, the first of
// classShaders that holds them, making them when no set of classes drew with
// them yet.
func (r *Renderer) classPipeFor(cl uint32) *classPipe {
	for i := range classShaders {
		s := &classShaders[i]
		if cl&^s.classes != 0 {
			continue
		}
		p := r.classes[s.classes]
		if p == nil {
			p = &classPipe{dual: s.classes&(1<<gpu.ClassSubpixel) != 0}
			vs, ps, err := s.vs, s.ps, error(nil)
			if r.compileClasses {
				vs, ps, err = compileClass(s.classes)
			}
			if err == nil && (failed(call(r.device, devCreateVertexShader, uintptr(unsafe.Pointer(&vs[0])), uintptr(len(vs)), 0, uintptr(unsafe.Pointer(&p.vs)))) ||
				failed(call(r.device, devCreatePixelShader, uintptr(unsafe.Pointer(&ps[0])), uintptr(len(ps)), 0, uintptr(unsafe.Pointer(&p.ps))))) {
				free(&p.vs)
				p.ps = 0
			}
			r.classes[s.classes] = p
		}
		r.classes[cl] = p
		return p
	}
	p := &classPipe{}
	r.classes[cl] = p
	return p
}

// bindShader binds what the shader of batch b reads: its uniforms, laid
// out as its effect says, in the constant buffer of register b2, which
// grows as needed, and its images in registers t6 to t9, its backdrop as
// one of them when it reads it.
func (r *Renderer) bindShader(b gpu.Batch) error {
	r.shaderBytes = b.Effect.Shader.Pack(r.shaderBytes, b.Uniforms)
	if n := len(r.shaderBytes); n > 0 {
		if n > r.shaderCBSize {
			free(&r.shaderCB)
			r.shaderCBSize = 0
			bd := bufferDesc{ByteWidth: uint32(n), Usage: usageDynamic, BindFlags: bindConstantBuffer, CPUAccessFlags: cpuAccessWrite}
			if hr := call(r.device, devCreateBuffer, uintptr(unsafe.Pointer(&bd)), 0, uintptr(unsafe.Pointer(&r.shaderCB))); failed(hr) {
				return fmt.Errorf("d3d11: cannot create the constant buffer of shaders: %#x", uint32(hr))
			}
			r.shaderCBSize = n
		}
		var m mapped
		if hr := call(r.ctx, ctxMap, r.shaderCB, 0, mapWriteDiscard, 0, uintptr(unsafe.Pointer(&m))); failed(hr) {
			return fmt.Errorf("d3d11: cannot map the constant buffer of shaders: %#x", uint32(hr))
		}
		copy(unsafe.Slice((*byte)(ptr(m.Data)), n), r.shaderBytes)
		call(r.ctx, ctxUnmap, r.shaderCB, 0)
		call(r.ctx, ctxPSSetConstantBuffers, 2, 1, uintptr(unsafe.Pointer(&r.shaderCB)))
	}
	views := b.Textures
	if b.Backdrop != 0 {
		// A shader reading its backdrop reads it as its image
		// BackdropImage.
		views[gpu.BackdropImage] = r.backdrop[0].srv
	}
	call(r.ctx, ctxPSSetShaderResources, 6, 4, uintptr(unsafe.Pointer(&views[0])))
	return nil
}

// effectShader is the pixel shader of an effect, or why it has none.
type effectShader struct {
	ps  uintptr
	err error
}

// effectShader returns the pixel shader of e: that e compiled ahead of
// time, unless it was compiled from another source, as when the
// renderer's head or tail changed since, which it then compiles. It is 0
// for an effect that does not compile, which draws nothing.
func (r *Renderer) effectShader(e *scene.Effect) uintptr {
	if p := r.effects[e]; p != nil {
		return p.ps
	}
	p := &effectShader{}
	if r.effects == nil {
		r.effects = map[*scene.Effect]*effectShader{}
	}
	r.effects[e] = p
	code := e.HLSL.Compiled
	if len(code) == 0 || e.HLSL.Sum != gpu.SourceSum(EffectSource(e.HLSL.Source)) {
		code, p.err = compileEffectCode(e.HLSL.Source)
	}
	if p.err == nil {
		if hr := call(r.device, devCreatePixelShader, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(&p.ps))); failed(hr) {
			p.err = fmt.Errorf("d3d11: cannot create the pixel shader of the effect %s: %#x", e.Name, uint32(hr))
		}
	} else {
		p.err = fmt.Errorf("d3d11: the effect %s: %w", e.Name, p.err)
	}
	if p.err != nil {
		// Shown once, as frames go on without the effect.
		fmt.Fprintln(os.Stderr, p.err)
	}
	return p.ps
}

// bindState sets the state the instances of s draw with, into the back
// buffer, with the backdrop of an effect.
func (r *Renderer) bindState(s *scene.Scene, backdrop uintptr) {
	ctx := r.ctx
	call(ctx, ctxOMSetRenderTargets, 1, uintptr(unsafe.Pointer(&r.drawing)), 0)
	vp := viewport{W: float32(s.Width), H: float32(s.Height), MaxDepth: 1}
	call(ctx, ctxRSSetViewports, 1, uintptr(unsafe.Pointer(&vp)))
	call(ctx, ctxOMSetBlendState, r.blend, 0, 0xffffffff)
	call(ctx, ctxIASetInputLayout, r.layout)
	stride, offset := uint32(gpu.InstanceSize), uint32(0)
	call(ctx, ctxIASetVertexBuffers, 0, 1, uintptr(unsafe.Pointer(&r.instBuf)), uintptr(unsafe.Pointer(&stride)), uintptr(unsafe.Pointer(&offset)))
	call(ctx, ctxVSSetShader, r.vs, 0, 0)
	call(ctx, ctxVSSetConstantBuffers, 0, 1, uintptr(unsafe.Pointer(&r.cbuf)))
	call(ctx, ctxPSSetShader, r.ps, 0, 0)
	call(ctx, ctxPSSetSamplers, 0, 1, uintptr(unsafe.Pointer(&r.samp)))
	views := [4]uintptr{r.mask.srv, r.color.srv, 0, backdrop}
	call(ctx, ctxPSSetShaderResources, 0, 4, uintptr(unsafe.Pointer(&views[0])))
	call(ctx, ctxPSSetShaderResources, 10, 1, uintptr(unsafe.Pointer(&r.stopsSRV)))
}

// syncStops uploads the stops of the gradients b built into r.stops,
// whose view r.stopsSRV then is, or 0 for none.
func (r *Renderer) syncStops(b *gpu.Builder) error {
	r.stopsSRV = 0
	w, h := b.StopsSize()
	if w == 0 {
		return nil
	}
	data := b.Stops
	for len(data) < 4*w*h {
		data = append(data, 0)
	}
	b.Stops = data[:len(b.Stops)]
	bytes := unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), 4*len(data))
	if r.stops.tex == 0 || r.stops.w != w || r.stops.h != h {
		if err := r.newTexture(&r.stops, w, h, formatR32G32B32A32Float, bytes, 16); err != nil {
			return err
		}
	} else {
		call(r.ctx, ctxUpdateSubresource, r.stops.tex, 0, 0, uintptr(unsafe.Pointer(&bytes[0])), uintptr(w*16), 0)
	}
	r.stopsSRV = r.stops.srv
	return nil
}

// blurImage computes the area of img that bk says blurred
// (gpu.Builder.BlurredImage) into a texture of the frame's own, as
// internal/raster computes it, and returns its view, or 0. drawScene sets
// again the state its passes change.
func (r *Renderer) blurImage(img *scene.Image, bk scene.Backdrop) uintptr {
	src := r.imageView(img)
	w, h := bk.Size()
	if src == 0 || w <= 0 || h <= 0 {
		return 0
	}
	for i := range r.blurTmp {
		if t := &r.blurTmp[i]; t.w < w || t.h < h {
			if r.newTarget(t, max(w, t.w), max(h, t.h), true) != nil {
				return 0
			}
		}
	}
	if r.used == len(r.blurred) {
		r.blurred = append(r.blurred, texture{})
	}
	out := &r.blurred[r.used]
	r.used++
	if out.tex == 0 || out.w != w || out.h != h {
		if r.newTarget(out, w, h, true) != nil {
			return 0
		}
	}
	ctx := r.ctx
	// No texture drawn into may be bound for reading.
	var none [5]uintptr
	call(ctx, ctxPSSetShaderResources, 0, 5, uintptr(unsafe.Pointer(&none[0])))
	call(ctx, ctxOMSetBlendState, 0, 0, 0xffffffff)
	call(ctx, ctxRSSetState, r.raster)
	call(ctx, ctxIASetPrimitiveTopology, topologyTriangleStrip)
	call(ctx, ctxIASetInputLayout, 0)
	call(ctx, ctxVSSetShader, r.passVS, 0, 0)
	call(ctx, ctxPSSetConstantBuffers, 1, 1, uintptr(unsafe.Pointer(&r.passCB)))
	if bk.Radius == 0 {
		r.pass(r.downPS, src, out, w, h, gpu.ImageDownPass(bk))
		return out.srv
	}
	r.pass(r.downPS, src, &r.blurTmp[0], w, h, gpu.ImageDownPass(bk))
	r.pass(r.blurPS, r.blurTmp[0].srv, &r.blurTmp[1], w, h, gpu.ImageBlurPass(bk, [2]int32{1, 0}))
	r.pass(r.blurPS, r.blurTmp[1].srv, out, w, h, gpu.ImageBlurPass(bk, [2]int32{0, 1}))
	return out.srv
}

// readBackdrop computes the backdrop bk of what the target drawn holds,
// into r.backdrop[0]: it copies the area into grab, averages its squares
// and blurs them.
func (r *Renderer) readBackdrop(bk scene.Backdrop) {
	ctx := r.ctx
	// No texture drawn into may be bound for reading.
	var none [5]uintptr
	call(ctx, ctxPSSetShaderResources, 0, 5, uintptr(unsafe.Pointer(&none[0])))
	var back uintptr
	call(r.drawing, viewGetResource, uintptr(unsafe.Pointer(&back)))
	from := box{Left: uint32(bk.Area.Min.X), Top: uint32(bk.Area.Min.Y), Right: uint32(bk.Area.Max.X), Bottom: uint32(bk.Area.Max.Y), Back: 1}
	call(ctx, ctxCopySubresourceRegion, r.grab.tex, 0, 0, 0, 0, back, 0, uintptr(unsafe.Pointer(&from)))
	free(&back)
	call(ctx, ctxOMSetBlendState, 0, 0, 0xffffffff)
	call(ctx, ctxIASetInputLayout, 0)
	call(ctx, ctxVSSetShader, r.passVS, 0, 0)
	call(ctx, ctxPSSetConstantBuffers, 1, 1, uintptr(unsafe.Pointer(&r.passCB)))
	w, h := bk.Size()
	r.pass(r.downPS, r.grab.srv, &r.backdrop[0], w, h, gpu.DownPass(bk, [2]int32{int32(bk.Area.Min.X), int32(bk.Area.Min.Y)}))
	if bk.Radius > 0 {
		r.pass(r.blurPS, r.backdrop[0].srv, &r.backdrop[1], w, h, gpu.BlurPass(bk, [2]int32{1, 0}))
		r.pass(r.blurPS, r.backdrop[1].srv, &r.backdrop[0], w, h, gpu.BlurPass(bk, [2]int32{0, 1}))
	}
}

// pass draws ps into the w×h texels at the start of dst, reading src.
func (r *Renderer) pass(ps, src uintptr, dst *texture, w, h int, p gpu.Pass) {
	ctx := r.ctx
	call(ctx, ctxUpdateSubresource, r.passCB, 0, 0, uintptr(unsafe.Pointer(&p)), 0, 0)
	call(ctx, ctxOMSetRenderTargets, 1, uintptr(unsafe.Pointer(&dst.rtv)), 0)
	vp := viewport{W: float32(dst.w), H: float32(dst.h), MaxDepth: 1}
	call(ctx, ctxRSSetViewports, 1, uintptr(unsafe.Pointer(&vp)))
	sc := gpu.Scissor{Right: int32(w), Bottom: int32(h)}
	call(ctx, ctxRSSetScissorRects, 1, uintptr(unsafe.Pointer(&sc)))
	call(ctx, ctxPSSetShader, ps, 0, 0)
	call(ctx, ctxPSSetShaderResources, 4, 1, uintptr(unsafe.Pointer(&src)))
	call(ctx, ctxDrawInstanced, 4, 1, 0, 0)
	var none uintptr
	call(ctx, ctxPSSetShaderResources, 4, 1, uintptr(unsafe.Pointer(&none)))
}

// fitBackdrop makes the textures of backdrops as large as the scene built
// needs, and the texture the area read is copied to.
func (r *Renderer) fitBackdrop(b *gpu.Builder) error {
	w, h := b.BackdropSize()
	aw, ah := 0, 0
	for _, bk := range b.Backdrops {
		aw, ah = max(aw, bk.Area.Dx()), max(ah, bk.Area.Dy())
	}
	// Somewhat larger, so that a pane growing does not make them again
	// every frame.
	if aw > r.grab.w || ah > r.grab.h {
		if err := r.newTarget(&r.grab, max(aw+aw/4, r.grab.w), max(ah+ah/4, r.grab.h), false); err != nil {
			return err
		}
	}
	if w > r.backdrop[0].w || h > r.backdrop[0].h {
		w, h = max(w+w/4, r.backdrop[0].w), max(h+h/4, r.backdrop[0].h)
		for i := range r.backdrop {
			if err := r.newTarget(&r.backdrop[i], w, h, true); err != nil {
				return err
			}
		}
	}
	return nil
}

// newTarget makes t a w×h texture of the back buffer's format, which
// shaders read and, when drawn, draw into.
func (r *Renderer) newTarget(t *texture, w, h int, drawn bool) error {
	free(&t.rtv)
	free(&t.srv)
	free(&t.tex)
	*t = texture{}
	desc := texture2DDesc{Width: uint32(w), Height: uint32(h), MipLevels: 1, ArraySize: 1, Format: formatB8G8R8A8Unorm, SampleCount: 1,
		Usage: usageDefault, BindFlags: bindShaderResource}
	if drawn {
		desc.BindFlags |= bindRenderTarget
	}
	if hr := call(r.device, devCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&t.tex))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create a %dx%d backdrop texture: %#x", w, h, uint32(hr))
	}
	if hr := call(r.device, devCreateShaderResourceView, t.tex, 0, uintptr(unsafe.Pointer(&t.srv))); failed(hr) {
		return fmt.Errorf("d3d11: cannot create a backdrop texture view: %#x", uint32(hr))
	}
	if drawn {
		if hr := call(r.device, devCreateRenderTargetView, t.tex, 0, uintptr(unsafe.Pointer(&t.rtv))); failed(hr) {
			return fmt.Errorf("d3d11: cannot create a backdrop render target: %#x", uint32(hr))
		}
	}
	t.w, t.h = w, h
	return nil
}

// Ready reports whether a frame following the last one can draw at once,
// as none waits to present (latency.go); otherwise wake runs, on the main
// thread, once it has.
func (r *Renderer) Ready(wake func()) bool {
	if r.pending || r.refreshing {
		r.wake = wake
		return false
	}
	if r.presentLater != nil && r.cost.fits() && !r.lat.ready() {
		r.wake = wake
		r.lat.waitThen(r.wakeLater)
		return false
	}
	r.cost.start = time.Now()
	return true
}

// SetPost gives the renderer post, which runs a function on the main
// thread from any goroutine: a frame drawn while the screen has no room
// for it presents once it has, through post, rather than block in Present.
// Without it, frames block.
func (r *Renderer) SetPost(post func(func())) {
	r.post = post
	r.presentLater = func() { post(r.presentPending) }
	r.wakeLater = func() { post(r.woke) }
	r.cost.readPeriod()
	if !r.cost.makeQueries(r.device) {
		// Nothing tells what frames cost: they build ahead.
		r.cost.estimate = time.Hour
	}
}

// presentPending presents the frame drawn while the screen had no room
// for it, waiting for room when called before the goroutine waiting for
// it is done.
func (r *Renderer) presentPending() {
	if !r.pending {
		return
	}
	r.pending = false
	if err := r.present(1); err != nil && r.failed == nil {
		r.failed = err // the next frame finds it
	}
	if r.cost.fits() {
		// Woken now, the next frame builds before the screen has room, and
		// waits to present too, and so would every one after it.
		r.waitComposed()
		return
	}
	r.woke()
}

// woke runs what Ready was given.
func (r *Renderer) woke() {
	if w := r.wake; w != nil {
		r.wake = nil
		r.cost.start = time.Now()
		w()
	}
}

// awaitRefresh returns once the display's next refresh shows; tests
// replace it. DwmFlush may return at once when DWM has nothing to
// compose, or wait until it next has, so the wait lasts at least
// least and at most most.
var awaitRefresh = func(least, most time.Duration) {
	start := time.Now()
	timer := time.NewTimer(most)
	defer timer.Stop()
	select {
	case <-dwmFlushed():
	case <-timer.C:
	}
	if d := least - time.Since(start); d > 0 {
		time.Sleep(d)
	}
}

// dwmFlush is the wait on DwmFlush under way, which every wait for a refresh
// shares, one that outlasted its bounds included: a window manager that takes
// its time holds one thread, not one for each frame meanwhile.
var dwmFlush struct {
	sync.Mutex
	done chan struct{}
}

// dwmFlushed returns a channel that closes once the window manager composed.
func dwmFlushed() <-chan struct{} {
	dwmFlush.Lock()
	defer dwmFlush.Unlock()
	if dwmFlush.done == nil {
		done := make(chan struct{})
		dwmFlush.done = done
		go func() {
			procDwmFlush.Call()
			dwmFlush.Lock()
			dwmFlush.done = nil
			dwmFlush.Unlock()
			close(done)
		}()
	}
	return dwmFlush.done
}

// minRefresh and maxRefresh bound awaitRefresh's wait: a 60 Hz refresh's
// period, as frames that show nothing new need come no faster, and a few
// of them, so that an animation goes on when DWM composes nothing.
const (
	minRefresh = time.Second / 60
	maxRefresh = 4 * minRefresh
)

// waitRefresh has the frame following one that changed nothing, which
// presents nothing to wait for, wait for the display's next refresh as a
// frame presented would (Ready), a goroutine waiting for it meanwhile:
// otherwise frames that change nothing, as an animation's moving less than
// a pixel, follow each other as fast as the main thread draws them.
func (r *Renderer) waitRefresh() {
	r.awaitDisplay(minRefresh, maxRefresh)
}

// waitComposed has the frame following one presented with sync interval 0 wait
// for the composition that shows it (Ready): at least half a refresh, in case
// DWM has nothing to compose and returns at once, whatever the display's rate,
// and at most two, so that a composition missed never stops an animation.
func (r *Renderer) waitComposed() {
	period := r.cost.refreshPeriod()
	r.awaitDisplay(period/2, 2*period)
}

// awaitDisplay has a goroutine wait for the display's next refresh, within its
// bounds, and tell Ready's caller on the main thread.
func (r *Renderer) awaitDisplay(least, most time.Duration) {
	if r.post == nil || r.refreshing {
		return
	}
	r.refreshing = true
	post := r.post
	go func() {
		awaitRefresh(least, most)
		post(r.refreshed)
	}()
}

// refreshed runs on the main thread once the refresh waitRefresh waited
// for came.
func (r *Renderer) refreshed() {
	if !r.refreshing {
		return
	}
	r.refreshing = false
	if r.wake != nil && r.presentLater != nil && r.cost.fits() && !r.lat.ready() {
		// It still builds once the screen has room.
		r.lat.waitThen(r.wakeLater)
		return
	}
	r.woke()
}

// dxgiStatusOccluded is DXGI_STATUS_OCCLUDED, which Present returns, as a
// success, when the window shows nowhere.
const dxgiStatusOccluded = 0x087A0001

// present shows the back buffer, after sync vertical blanks, once the
// screen has room for it: what the frame drew, when it drew in part, and
// DXGI the rest of the last frame.
func (r *Renderer) present(sync uintptr) error {
	r.lat.take()
	return r.show(sync)
}

// show presents the back buffer, its token taken, or none needed.
func (r *Renderer) show(sync uintptr) error {
	var hr uintptr
	if r.redrew == redrewPart {
		params := presentParameters{DirtyRectsCount: uint32(len(r.part.Rects)), DirtyRects: unsafe.SliceData(r.part.Rects)}
		hr = call(r.swapChain, scPresent1, sync, 0, uintptr(unsafe.Pointer(&params)))
	} else {
		hr = call(r.swapChain, scPresent, sync, 0)
	}
	if failed(hr) {
		return fmt.Errorf("d3d11: present failed: %#x", uint32(hr))
	}
	// A frame presented while the window shows nowhere, as the first of a
	// window made hidden, may not be what the window shows once it does:
	// the next frame draws whole.
	r.held = r.ctx1 != 0 && uint32(hr) != dxgiStatusOccluded
	if r.frame-r.swept >= sweepEvery {
		r.swept = r.frame
		r.sweep()
		r.cost.readPeriod()
	}
	return nil
}

// FrameInfo describes the last frame for MYGO_FRAME_STATS.
func (r *Renderer) FrameInfo() string {
	switch r.redrew {
	case redrewPart:
		return fmt.Sprintf("redrew %s, %s", r.part.String(), r.b.Summary())
	case redrewNothing:
		return "unchanged"
	}
	return r.b.Summary()
}

// sweepEvery is how many frames go by between sweeps. show sweeps once
// that many went by since the last sweep, rather than at each multiple of
// it: frames that change nothing count too and present nothing, so an
// animation that changes every other frame could present none of the
// multiples, and never sweep.
const sweepEvery = 120

// sweep frees the textures of images no frame drew for a while, and
// those of canvases gone, or transient and not drawn this frame.
func (r *Renderer) sweep() {
	for id, t := range r.images {
		if r.frame-t.lastFrame > 240 {
			free(&t.srv)
			free(&t.tex)
			delete(r.images, id)
		}
	}
	for id, t := range r.canvases {
		if c := t.canvas.Value(); c == nil || c.Transient && t.lastFrame != r.frame {
			freeCanvas(t)
			delete(r.canvases, id)
		}
	}
}

// meets reports whether sc meets one of areas.
func meets(sc gpu.Scissor, areas []gpu.Scissor) bool {
	for _, a := range areas {
		if !sc.Intersect(a).Empty() {
			return true
		}
	}
	return false
}
