package main

import (
	"bytes"
	_ "embed"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func init() { runtime.LockOSThread() }

//go:embed art/logo.png
var logoPNG []byte

var (
	wUser32  = windows.NewLazySystemDLL("user32.dll")
	wGdi32   = windows.NewLazySystemDLL("gdi32.dll")
	wGdiplus = windows.NewLazySystemDLL("gdiplus.dll")
	wDwm     = windows.NewLazySystemDLL("dwmapi.dll")
	wShell32 = windows.NewLazySystemDLL("shell32.dll")
	wOle32   = windows.NewLazySystemDLL("ole32.dll")
	wKernel  = windows.NewLazySystemDLL("kernel32.dll")

	uRegisterClassEx  = wUser32.NewProc("RegisterClassExW")
	uCreateWindowEx   = wUser32.NewProc("CreateWindowExW")
	uDefWindowProc    = wUser32.NewProc("DefWindowProcW")
	uGetMessage       = wUser32.NewProc("GetMessageW")
	uTranslateMessage = wUser32.NewProc("TranslateMessage")
	uDispatchMessage  = wUser32.NewProc("DispatchMessageW")
	uPostQuitMessage  = wUser32.NewProc("PostQuitMessage")
	uPostMessage      = wUser32.NewProc("PostMessageW")
	uShowWindow       = wUser32.NewProc("ShowWindow")
	uLoadCursor       = wUser32.NewProc("LoadCursorW")
	uLoadIcon         = wUser32.NewProc("LoadIconW")
	uSetCursor        = wUser32.NewProc("SetCursor")
	uGetDC            = wUser32.NewProc("GetDC")
	uReleaseDC        = wUser32.NewProc("ReleaseDC")
	uBeginPaint       = wUser32.NewProc("BeginPaint")
	uEndPaint         = wUser32.NewProc("EndPaint")
	uInvalidateRect   = wUser32.NewProc("InvalidateRect")
	uGetClientRect    = wUser32.NewProc("GetClientRect")
	uAdjustWindowRect = wUser32.NewProc("AdjustWindowRectEx")
	uSetTimer         = wUser32.NewProc("SetTimer")
	uSetDPIAware      = wUser32.NewProc("SetProcessDPIAware")
	uSetDPIContext    = wUser32.NewProc("SetProcessDpiAwarenessContext")
	uGetDpiForWindow  = wUser32.NewProc("GetDpiForWindow")
	uTrackMouse       = wUser32.NewProc("TrackMouseEvent")
	uSysParamsInfo    = wUser32.NewProc("SystemParametersInfoW")
	uSetForeground    = wUser32.NewProc("SetForegroundWindow")
	uMessageBox       = wUser32.NewProc("MessageBoxW")
	uSetWindowPos     = wUser32.NewProc("SetWindowPos")
	uOpenClipboard    = wUser32.NewProc("OpenClipboard")
	uCloseClipboard   = wUser32.NewProc("CloseClipboard")
	uGetClipboardData = wUser32.NewProc("GetClipboardData")

	gCreateCompatibleDC = wGdi32.NewProc("CreateCompatibleDC")
	gCreateDIBSection   = wGdi32.NewProc("CreateDIBSection")
	gSelectObject       = wGdi32.NewProc("SelectObject")
	gDeleteObject       = wGdi32.NewProc("DeleteObject")
	gDeleteDC           = wGdi32.NewProc("DeleteDC")
	gBitBlt             = wGdi32.NewProc("BitBlt")
	gCreateFont         = wGdi32.NewProc("CreateFontIndirectW")
	gSetTextColor       = wGdi32.NewProc("SetTextColor")
	gSetBkMode          = wGdi32.NewProc("SetBkMode")
	gDrawText           = wUser32.NewProc("DrawTextW")
	gGetDeviceCaps      = wGdi32.NewProc("GetDeviceCaps")

	pStartup          = wGdiplus.NewProc("GdiplusStartup")
	pCreateFromHDC    = wGdiplus.NewProc("GdipCreateFromHDC")
	pDeleteGraphics   = wGdiplus.NewProc("GdipDeleteGraphics")
	pSetSmoothing     = wGdiplus.NewProc("GdipSetSmoothingMode")
	pSetPixelOffset   = wGdiplus.NewProc("GdipSetPixelOffsetMode")
	pCreateSolidFill  = wGdiplus.NewProc("GdipCreateSolidFill")
	pDeleteBrush      = wGdiplus.NewProc("GdipDeleteBrush")
	pFillRectangleI   = wGdiplus.NewProc("GdipFillRectangleI")
	pFillEllipseI     = wGdiplus.NewProc("GdipFillEllipseI")
	pCreatePath       = wGdiplus.NewProc("GdipCreatePath")
	pDeletePath       = wGdiplus.NewProc("GdipDeletePath")
	pAddPathArcI      = wGdiplus.NewProc("GdipAddPathArcI")
	pClosePathFigure  = wGdiplus.NewProc("GdipClosePathFigure")
	pFillPath         = wGdiplus.NewProc("GdipFillPath")
	pGetDC            = wGdiplus.NewProc("GdipGetDC")
	pReleaseDC        = wGdiplus.NewProc("GdipReleaseDC")
	pCreatePen        = wGdiplus.NewProc("GdipCreatePen1")
	pDeletePen        = wGdiplus.NewProc("GdipDeletePen")
	pDrawLinesI       = wGdiplus.NewProc("GdipDrawLinesI")
	pSetPenLineJoin   = wGdiplus.NewProc("GdipSetPenLineJoin")
	pSetPenStartCap   = wGdiplus.NewProc("GdipSetPenStartCap")
	pSetPenEndCap     = wGdiplus.NewProc("GdipSetPenEndCap")
	pBitmapFromScan0  = wGdiplus.NewProc("GdipCreateBitmapFromScan0")
	pDrawImageRectI   = wGdiplus.NewProc("GdipDrawImageRectI")
	pSetInterpolation = wGdiplus.NewProc("GdipSetInterpolationMode")
	uLoadImage        = wUser32.NewProc("LoadImageW")
	uGetSysMetrics    = wUser32.NewProc("GetSystemMetrics")
	dSetWindowAttr    = wDwm.NewProc("DwmSetWindowAttribute")
	sBrowseForFolder  = wShell32.NewProc("SHBrowseForFolderW")
	sPathFromIDList   = wShell32.NewProc("SHGetPathFromIDListW")
	sShellExecute     = wShell32.NewProc("ShellExecuteW")
	oCoTaskMemFree    = wOle32.NewProc("CoTaskMemFree")
	oCoInitializeEx   = wOle32.NewProc("CoInitializeEx")
	kGetModuleHandle  = wKernel.NewProc("GetModuleHandleW")
	kGlobalLock       = wKernel.NewProc("GlobalLock")
	kGlobalUnlock     = wKernel.NewProc("GlobalUnlock")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsMaximizeBox      = 0x00010000
	csHRedraw          = 0x2
	csVRedraw          = 0x1

	wmDestroy       = 0x0002
	wmSize          = 0x0005
	wmPaint         = 0x000F
	wmClose         = 0x0010
	wmEraseBkgnd    = 0x0014
	wmSetCursor     = 0x0020
	wmGetMinMaxInfo = 0x0024
	wmKeyDown       = 0x0100
	wmChar          = 0x0102
	wmTimer         = 0x0113
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmMouseWheel    = 0x020A
	wmMouseLeave    = 0x02A3
	wmDpiChanged    = 0x02E0
	wmApp           = 0x8000

	dtLeft       = 0x0
	dtCenter     = 0x1
	dtRight      = 0x2
	dtVCenter    = 0x4
	dtWordBreak  = 0x10
	dtSingleLine = 0x20
	dtCalcRect   = 0x400
	dtNoPrefix   = 0x800
	dtPathEll    = 0x4000
	dtEndEll     = 0x8000

	vkReturn = 0x0D
	vkEscape = 0x1B
	vkUp     = 0x26
	vkDown   = 0x28
	vkF5     = 0x74
)

const (
	cBg       = 0x16161A
	cSide     = 0x111114
	cFoot     = 0x0F0F12
	cPanel    = 0x1C1C21
	cPanelHi  = 0x222228
	cLine     = 0x2C2C33
	cEdge     = 0x3A3A42
	cText     = 0xD9D6CC
	cBright   = 0xF1EEE4
	cMuted    = 0x8A877D
	cGold     = 0xE8B84A
	cGoldHi   = 0xF2C862
	cGoldBg   = 0x26231A
	cGoldTint = 0x2B2414
	cGoldEdge = 0x6B5520
	cOnGold   = 0x1A1405
	cGreen    = 0x7FC97F
	cGreenBg  = 0x1B261C
	cRed      = 0xE6705E
	cRedBg    = 0x2C1B19
	cKnob     = 0xC9C6BC
)

type wndClassEx struct {
	size       uint32
	style      uint32
	proc       uintptr
	clsExtra   int32
	wndExtra   int32
	inst       uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menu       *uint16
	name       *uint16
	iconSm     uintptr
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

type winRect struct{ left, top, right, bottom int32 }

type paintStruct struct {
	hdc       uintptr
	erase     int32
	rc        winRect
	restore   int32
	incUpdate int32
	reserved  [32]byte
}

type logFontW struct {
	height, width, escapement, orientation, weight       int32
	italic, underline, strikeOut, charSet                byte
	outPrecision, clipPrecision, quality, pitchAndFamily byte
	face                                                 [32]uint16
}

type bmpInfo struct {
	size                   uint32
	width, height          int32
	planes, bitCount       uint16
	compression, imageSize uint32
	xppm, yppm             int32
	clrUsed, clrImportant  uint32
	colors                 [4]uint32
}

type trackMouse struct {
	size  uint32
	flags uint32
	hwnd  uintptr
	hover uint32
}

type winPoint struct{ x, y int32 }

type minMaxInfo struct {
	reserved, maxSize, maxPos, minTrack, maxTrack winPoint
}

type browseInfoW struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	fn          uintptr
	lParam      uintptr
	image       int32
}

type gdipInput struct {
	version  uint32
	callback uintptr
	noThread int32
	noCodecs int32
}

type box struct{ x, y, w, h int32 }

func (b box) has(x, y int32) bool { return x >= b.x && y >= b.y && x < b.x+b.w && y < b.y+b.h }

func (b box) cut(c box) box {
	x0, y0 := max(b.x, c.x), max(b.y, c.y)
	x1, y1 := min(b.x+b.w, c.x+c.w), min(b.y+b.h, c.y+c.h)
	if x1 <= x0 || y1 <= y0 {
		return box{}
	}
	return box{x0, y0, x1 - x0, y1 - y0}
}

type hit struct {
	b  box
	id string
	on bool
}

type gui struct {
	app        *app
	hwnd       uintptr
	inst       uintptr
	dpi        int32
	fonts      map[string]uintptr
	measure    uintptr
	g          uintptr
	dc         uintptr
	w, h       int32
	hits       []hit
	clip       box
	hover      string
	press      string
	tracked    bool
	scroll     int32
	content    int32
	view       int32
	tick       int
	shot       string
	shotKey    string
	shotTest   bool
	started    time.Time
	armed      bool
	restarting bool
	logo       uintptr
	logoPix    []byte
	hookText   string
	hookFocus  bool
	hookInit   bool
}

var theGUI *gui

func u16p(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func f32(v float32) uintptr { return uintptr(math.Float32bits(v)) }

func argb(c uint32) uintptr { return uintptr(0xFF000000 | c) }

func colorref(c uint32) uintptr {
	return uintptr((c&0xFF)<<16 | c&0xFF00 | c>>16&0xFF)
}

func (u *gui) s(v int32) int32 { return v * u.dpi / 96 }

func (u *gui) mkFont(pt int32, weight int32) uintptr {
	lf := logFontW{height: -pt * u.dpi / 72, weight: weight, charSet: 1, quality: 5}
	copy(lf.face[:], syscall.StringToUTF16("Segoe UI"))
	f, _, _ := gCreateFont.Call(uintptr(unsafe.Pointer(&lf)))
	return f
}

func (u *gui) makeFonts() {
	for _, f := range u.fonts {
		gDeleteObject.Call(f)
	}
	u.fonts = map[string]uintptr{
		"logo":  u.mkFont(16, 700),
		"title": u.mkFont(17, 600),
		"head":  u.mkFont(11, 600),
		"body":  u.mkFont(10, 400),
		"bold":  u.mkFont(10, 600),
		"small": u.mkFont(9, 400),
		"btn":   u.mkFont(10, 600),
		"mark":  u.mkFont(10, 700),
	}
}

func (u *gui) loadLogo() {
	img, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			i := (y*w + x) * 4
			a := uint32(c.A)
			pix[i], pix[i+1], pix[i+2], pix[i+3] = byte(uint32(c.B)*a/255), byte(uint32(c.G)*a/255), byte(uint32(c.R)*a/255), c.A
		}
	}
	u.logoPix = pix
	pBitmapFromScan0.Call(uintptr(w), uintptr(h), uintptr(w*4), 0xE200B, uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&u.logo)))
}

func (u *gui) image(b box) {
	if u.logo == 0 {
		return
	}
	pSetInterpolation.Call(u.g, 7)
	pDrawImageRectI.Call(u.g, u.logo, uintptr(b.x), uintptr(b.y), uintptr(b.w), uintptr(b.h))
}

func (u *gui) fill(b box, c uint32) {
	var br uintptr
	pCreateSolidFill.Call(argb(c), uintptr(unsafe.Pointer(&br)))
	pFillRectangleI.Call(u.g, br, uintptr(b.x), uintptr(b.y), uintptr(b.w), uintptr(b.h))
	pDeleteBrush.Call(br)
}

func (u *gui) round(b box, r int32, c uint32) {
	if b.w <= 0 || b.h <= 0 {
		return
	}
	d := min(2*r, b.w, b.h)
	if d <= 1 {
		u.fill(b, c)
		return
	}
	var path, br uintptr
	pCreatePath.Call(0, uintptr(unsafe.Pointer(&path)))
	pAddPathArcI.Call(path, uintptr(b.x), uintptr(b.y), uintptr(d), uintptr(d), f32(180), f32(90))
	pAddPathArcI.Call(path, uintptr(b.x+b.w-d-1), uintptr(b.y), uintptr(d), uintptr(d), f32(270), f32(90))
	pAddPathArcI.Call(path, uintptr(b.x+b.w-d-1), uintptr(b.y+b.h-d-1), uintptr(d), uintptr(d), f32(0), f32(90))
	pAddPathArcI.Call(path, uintptr(b.x), uintptr(b.y+b.h-d-1), uintptr(d), uintptr(d), f32(90), f32(90))
	pClosePathFigure.Call(path)
	pCreateSolidFill.Call(argb(c), uintptr(unsafe.Pointer(&br)))
	pFillPath.Call(u.g, br, path)
	pDeleteBrush.Call(br)
	pDeletePath.Call(path)
}

func (u *gui) framed(b box, r int32, bg, edge uint32) {
	u.round(b, r, edge)
	t := max(u.s(1), 1)
	u.round(box{b.x + t, b.y + t, b.w - 2*t, b.h - 2*t}, max(r-t, 1), bg)
}

func (u *gui) dot(cx, cy, r int32, c uint32) {
	var br uintptr
	pCreateSolidFill.Call(argb(c), uintptr(unsafe.Pointer(&br)))
	pFillEllipseI.Call(u.g, br, uintptr(cx-r), uintptr(cy-r), uintptr(2*r), uintptr(2*r))
	pDeleteBrush.Call(br)
}

func (u *gui) tick3(b box, c uint32) {
	var pen uintptr
	pCreatePen.Call(argb(c), f32(float32(u.s(2))+0.2), 2, uintptr(unsafe.Pointer(&pen)))
	pSetPenStartCap.Call(pen, 2)
	pSetPenEndCap.Call(pen, 2)
	pSetPenLineJoin.Call(pen, 2)
	pts := [6]int32{b.x + b.w*22/100, b.y + b.h*52/100, b.x + b.w*42/100, b.y + b.h*72/100, b.x + b.w*78/100, b.y + b.h*32/100}
	pDrawLinesI.Call(u.g, pen, uintptr(unsafe.Pointer(&pts[0])), 3)
	pDeletePen.Call(pen)
}

func (u *gui) text(b box, s, font string, c uint32, flags uint32) {
	if s == "" || b.w <= 0 || b.h <= 0 {
		return
	}
	var hdc uintptr
	pGetDC.Call(u.g, uintptr(unsafe.Pointer(&hdc)))
	old, _, _ := gSelectObject.Call(hdc, u.fonts[font])
	gSetBkMode.Call(hdc, 1)
	gSetTextColor.Call(hdc, colorref(c))
	r := winRect{b.x, b.y, b.x + b.w, b.y + b.h}
	gDrawText.Call(hdc, uintptr(unsafe.Pointer(u16p(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags|dtNoPrefix))
	gSelectObject.Call(hdc, old)
	pReleaseDC.Call(u.g, hdc)
}

func (u *gui) line(b box, s, font string, c uint32) {
	u.text(b, s, font, c, dtLeft|dtVCenter|dtSingleLine|dtEndEll)
}

func (u *gui) size(s, font string, w int32) (int32, int32) {
	old, _, _ := gSelectObject.Call(u.measure, u.fonts[font])
	r := winRect{0, 0, w, 0}
	flags := uintptr(dtCalcRect | dtNoPrefix)
	if w > 0 {
		flags |= dtWordBreak
	} else {
		flags |= dtSingleLine
	}
	gDrawText.Call(u.measure, uintptr(unsafe.Pointer(u16p(s))), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
	gSelectObject.Call(u.measure, old)
	return r.right - r.left, r.bottom - r.top
}

func (u *gui) wrap(x, y, w int32, s, font string, c uint32) int32 {
	if s == "" {
		return 0
	}
	_, h := u.size(s, font, w)
	u.text(box{x, y, w, h + u.s(2)}, s, font, c, dtLeft|dtWordBreak)
	return h
}

func (u *gui) add(b box, id string, on bool) {
	if u.clip.w > 0 {
		b = b.cut(u.clip)
	}
	if b.w > 0 {
		u.hits = append(u.hits, hit{b: b, id: id, on: on})
	}
}

func (u *gui) hot(id string) bool { return id != "" && u.hover == id }

func (u *gui) button(b box, label, id string, on, primary bool) {
	u.add(b, id, on)
	r := u.s(7)
	switch {
	case !on:
		u.framed(b, r, cPanel, cLine)
		u.text(b, label, "btn", cMuted, dtCenter|dtVCenter|dtSingleLine)
	case primary:
		c := uint32(cGold)
		if u.hot(id) {
			c = cGoldHi
		}
		u.round(b, r, c)
		u.text(b, label, "btn", cOnGold, dtCenter|dtVCenter|dtSingleLine)
	default:
		bg := uint32(cPanel)
		if u.hot(id) {
			bg = cPanelHi
		}
		u.framed(b, r, bg, cEdge)
		u.text(b, label, "btn", cBright, dtCenter|dtVCenter|dtSingleLine)
	}
}

func (u *gui) btnWidth(label string) int32 {
	w, _ := u.size(label, "btn", 0)
	return max(w+u.s(36), u.s(110))
}

func kindColor(k int) uint32 {
	switch k {
	case kindOK:
		return cGreen
	case kindErr:
		return cRed
	case kindNew:
		return cGold
	}
	return cMuted
}

func (u *gui) progress(b box, got, total int64) {
	u.round(b, b.h/2, cLine)
	if total > 0 {
		w := int32(float64(b.w) * min(float64(got)/float64(total), 1))
		if w > 0 {
			u.round(box{b.x, b.y, max(w, b.h), b.h}, b.h/2, cGold)
		}
		return
	}
	seg := b.w / 4
	pos := int32(u.tick) * u.s(6) % (b.w + seg)
	x0, x1 := max(b.x+pos-seg, b.x), min(b.x+pos, b.x+b.w)
	if x1 > x0 {
		u.round(box{x0, b.y, x1 - x0, b.h}, b.h/2, cGold)
	}
}

func (u *gui) draw() {
	v := u.app.view()
	u.hits = u.hits[:0]
	u.clip = box{}
	side, foot := u.s(248), u.s(60)
	u.fill(box{0, 0, u.w, u.h}, cBg)
	area := box{side, 0, u.w - side, u.h - foot}
	u.view = area.h
	u.clip = area
	u.content = u.drawContent(v, area) + u.s(28)
	if top := u.content - u.view; u.scroll > top {
		u.scroll = max(0, top)
	}
	u.clip = box{}
	if u.content > u.view {
		track := box{u.w - u.s(6), u.s(8), u.s(3), u.view - u.s(16)}
		th := max(track.h*u.view/u.content, u.s(30))
		ty := track.y + (track.h-th)*u.scroll/max(u.content-u.view, 1)
		u.round(box{track.x, ty, track.w, th}, track.w/2, cEdge)
	}
	u.drawSide(v, box{0, 0, side, u.h - foot})
	u.drawFoot(v, box{0, u.h - foot, u.w, foot})
}

func (u *gui) drawSide(v appView, b box) {
	u.fill(b, cSide)
	u.fill(box{b.x + b.w - u.s(1), b.y, u.s(1), b.h}, cLine)
	x := b.x + u.s(22)
	u.image(box{x, u.s(18), u.s(44), u.s(44)})
	tx := x + u.s(56)
	u.line(box{tx, u.s(16), b.w - tx - u.s(12), u.s(30)}, "Manacode", "logo", cGold)
	u.line(box{tx, u.s(44), b.w - tx - u.s(12), u.s(18)}, "обновление аддонов", "small", cMuted)
	y := u.s(84)
	for i, c := range v.cards {
		item := box{b.x + u.s(12), y, b.w - u.s(24), u.s(62)}
		id := "card:" + strconv.Itoa(i)
		u.add(item, id, v.page == pageMain)
		switch {
		case i == v.cur:
			u.framed(item, u.s(8), cGoldBg, cGoldEdge)
		case u.hot(id):
			u.round(item, u.s(8), 0x1A1A1F)
		}
		name := uint32(cText)
		if i == v.cur {
			name = cGold
		}
		u.line(box{item.x + u.s(16), item.y + u.s(10), item.w - u.s(28), u.s(22)}, c.title, "head", name)
		dc := kindColor(c.dot)
		u.dot(item.x+u.s(20), item.y+u.s(43), u.s(4), dc)
		sc := uint32(cMuted)
		if c.dot != kindNone {
			sc = dc
		}
		u.line(box{item.x + u.s(31), item.y + u.s(33), item.w - u.s(42), u.s(20)}, c.short, "small", sc)
		y += u.s(68)
	}
	fy := b.y + b.h - u.s(78)
	u.fill(box{b.x + u.s(22), fy - u.s(12), b.w - u.s(44), u.s(1)}, cLine)
	u.line(box{x, fy, b.w - u.s(40), u.s(18)}, "Папка игры", "small", cMuted)
	path := v.addons
	if path == "" {
		path = "не найдена"
	}
	u.text(box{x, fy + u.s(18), b.w - u.s(44), u.s(20)}, path, "small", cText, dtLeft|dtVCenter|dtSingleLine|dtPathEll)
	lw, _ := u.size("Сменить…", "small", 0)
	link := box{x, fy + u.s(40), lw, u.s(20)}
	u.add(link, "folder", !v.busy)
	lc := uint32(cGold)
	if u.hot("folder") {
		lc = cGoldHi
	}
	u.line(link, "Сменить…", "small", lc)
}

func (u *gui) drawFoot(v appView, b box) {
	u.fill(b, cFoot)
	u.fill(box{b.x, b.y, b.w, u.s(1)}, cLine)
	cy := b.y + b.h/2
	tg := box{b.x + u.s(22), cy - u.s(11), u.s(40), u.s(22)}
	u.add(tg, "auto", v.autoOn)
	if v.auto {
		u.round(tg, tg.h/2, cGold)
		u.dot(tg.x+tg.w-tg.h/2, cy, tg.h/2-u.s(4), cOnGold)
	} else {
		bg := uint32(cEdge)
		if u.hot("auto") {
			bg = 0x4A4A54
		}
		u.round(tg, tg.h/2, bg)
		u.dot(tg.x+tg.h/2, cy, tg.h/2-u.s(4), cKnob)
	}
	x := tg.x + tg.w + u.s(14)
	title := "Автообновление выключено"
	if v.auto {
		title = "Автообновление включено"
	}
	u.line(box{x, cy - u.s(19), u.s(360), u.s(20)}, title, "bold", cBright)
	if v.autoErr != "" {
		u.line(box{x, cy + u.s(1), b.w/2 + u.s(80), u.s(18)}, v.autoErr, "small", cRed)
	} else {
		pre := "в фоне, без окон: данные — каждые 15 минут, аддоны — "
		pw, _ := u.size(pre, "small", 0)
		u.line(box{x, cy + u.s(1), pw + u.s(4), u.s(18)}, pre, "small", cMuted)
		hl := hoursRU(v.hours)
		hw, _ := u.size(hl, "small", 0)
		hb := box{x + pw, cy + u.s(1), hw, u.s(18)}
		u.add(hb, "hours", v.autoOn)
		hc := uint32(cGold)
		if u.hot("hours") {
			hc = cGoldHi
		}
		u.line(hb, hl, "small", hc)
	}
	right := b.x + b.w - u.s(22)
	topY, lowY := cy-u.s(20), cy+u.s(1)
	ver := "Версия " + v.version
	switch {
	case v.notice != "":
		w, _ := u.size(v.notice, "small", 0)
		u.text(box{right - w, topY, w, u.s(20)}, v.notice, "small", cRed, dtRight|dtVCenter|dtSingleLine)
	case v.selfVer != "":
		top := "Вышла ManacodeUpdate " + v.selfVer + " — скачать вручную"
		tw, _ := u.size(top, "small", 0)
		tb := box{right - tw, topY, tw, u.s(20)}
		u.add(tb, "self", true)
		tc := uint32(cGold)
		if u.hot("self") {
			tc = cGoldHi
		}
		u.text(tb, top, "small", tc, dtRight|dtVCenter|dtSingleLine)
	case v.game:
		u.gameNote(right, topY)
		v.game = false
	}
	if v.game {
		u.gameNote(right, lowY)
		return
	}
	u.text(box{right - u.s(200), lowY, u.s(200), u.s(20)}, ver, "small", cMuted, dtRight|dtVCenter|dtSingleLine)
}

func (u *gui) gameNote(right, y int32) {
	msg := "Игра запущена — установка подождёт, пока её закроют"
	w, _ := u.size(msg, "small", 0)
	u.dot(right-w-u.s(10), y+u.s(10), u.s(4), cRed)
	u.text(box{right - w, y, w, u.s(20)}, msg, "small", cRed, dtRight|dtVCenter|dtSingleLine)
}

func (u *gui) drawContent(v appView, area box) int32 {
	x, w := area.x+u.s(36), area.w-u.s(72)
	y0 := area.y + u.s(28) - u.scroll
	if v.page == pageTest {
		return u.drawTest(x, y0, w) - y0
	}
	if v.addons == "" {
		return u.drawWelcome(x, y0, w) - y0
	}
	c := v.cards[v.cur]
	y := y0
	u.line(box{x, y, w, u.s(34)}, c.title, "title", cBright)
	y += u.s(36)
	y += u.wrap(x, y, w, c.desc, "body", cMuted) + u.s(8)
	if c.warn != "" {
		y += u.wrap(x, y, w, c.warn, "bold", cRed) + u.s(6)
	}
	y += u.s(12)
	y = u.versionPanel(c, x, y, w)
	if c.note != "" {
		y = u.notice(x, y, w, c.note, c.noteKind) + u.s(4)
	}
	switch {
	case c.pri:
		y = u.priPanels(c, x, y+u.s(10), w)
	case c.rh:
		y = u.rhPanels(c, x, y+u.s(10), w)
	}
	return y + u.scroll - area.y
}

func (u *gui) versionPanel(c cardView, x, y, w int32) int32 {
	h := u.s(76)
	p := box{x, y, w, h}
	u.round(p, u.s(10), cPanel)
	bw := u.btnWidth(c.action)
	tx, tw := x+u.s(20), w-u.s(40)
	if c.action != "" {
		tw -= bw + u.s(16)
	}
	u.line(box{tx, y + u.s(12), tw, u.s(18)}, "Версия", "small", cMuted)
	u.line(box{tx, y + u.s(30), tw, u.s(24)}, c.ver, "head", cBright)
	vw, _ := u.size(c.ver, "head", 0)
	sub := c.verSub
	if c.busy {
		sub = ""
	}
	u.line(box{tx + vw + u.s(12), y + u.s(31), tw - vw - u.s(12), u.s(22)}, sub, "body", kindColor(c.verKind))
	if c.action != "" {
		u.button(box{x + w - u.s(20) - bw, y + (h-u.s(36))/2, bw, u.s(36)}, c.action, "act:"+c.key, c.actionOn, c.primary)
	}
	y += h
	if c.busy {
		y += u.s(10)
		u.round(box{x, y, w, u.s(54)}, u.s(10), cPanel)
		text := "Ставлю " + c.busyText
		if c.total > 0 {
			text += " · " + mb(c.got) + " из " + mb(c.total)
		} else if c.got > 0 {
			text += " · " + mb(c.got)
		}
		u.line(box{x + u.s(20), y + u.s(8), w - u.s(40), u.s(20)}, text, "small", cText)
		u.progress(box{x + u.s(20), y + u.s(34), w - u.s(40), u.s(6)}, c.got, c.total)
		y += u.s(54)
	}
	y += u.s(10)
	lx := x + u.s(4)
	for _, l := range []struct{ label, id, target string }{
		{"Открыть папку", "open:" + c.key + ":folder", c.folder},
		{"Страница на GitHub", "open:" + c.key + ":page", c.page},
		{"CurseForge", "open:" + c.key + ":curse", c.curse},
	} {
		if l.target == "" {
			continue
		}
		lw, _ := u.size(l.label, "small", 0)
		b := box{lx, y, lw, u.s(20)}
		u.add(b, l.id, true)
		lc := uint32(cMuted)
		if u.hot(l.id) {
			lc = cGold
		}
		u.line(b, l.label, "small", lc)
		lx += lw + u.s(22)
	}
	if lx > x+u.s(4) {
		y += u.s(30)
	}
	return y
}

func (u *gui) notice(x, y, w int32, text string, kind int) int32 {
	bg, fg := uint32(cGreenBg), uint32(cGreen)
	if kind == kindErr {
		bg, fg = cRedBg, cRed
	}
	_, th := u.size(text, "body", w-u.s(40))
	h := th + u.s(22)
	u.round(box{x, y, w, h}, u.s(10), bg)
	u.dot(x+u.s(18), y+u.s(11)+th/2, u.s(4), fg)
	u.text(box{x + u.s(32), y + u.s(11), w - u.s(48), th + u.s(2)}, text, "body", fg, dtLeft|dtWordBreak)
	return y + h + u.s(6)
}

func (u *gui) priPanels(c cardView, x, y, w int32) int32 {
	h := u.s(76)
	u.round(box{x, y, w, h}, u.s(10), cPanel)
	tx, tw := x+u.s(20), w-u.s(40)
	bw := u.btnWidth(c.dataAction)
	if c.dataAction != "" {
		tw -= bw + u.s(16)
	}
	u.line(box{tx, y + u.s(12), tw, u.s(18)}, "Данные", "small", cMuted)
	u.line(box{tx, y + u.s(30), tw, u.s(24)}, c.dataTitle, "head", cBright)
	dw, _ := u.size(c.dataTitle, "head", 0)
	u.line(box{tx + dw + u.s(12), y + u.s(31), tw - dw - u.s(12), u.s(22)}, c.dataSub, "body", kindColor(c.dataKind))
	if c.dataAction != "" {
		u.button(box{x + w - u.s(20) - bw, y + (h-u.s(36))/2, bw, u.s(36)}, c.dataAction, "data", c.dataOn, c.dataPrimary)
	}
	y += h + u.s(22)
	if len(c.seasons) > 0 {
		u.line(box{x, y, w, u.s(18)}, "Сезоны", "small", cMuted)
		y += u.s(26)
		cx := x
		ch := u.s(34)
		for _, s := range c.seasons {
			tw, _ := u.size(s.label, "bold", 0)
			cw := tw + u.s(28)
			if s.on {
				cw += u.s(20)
			}
			if cx+cw > x+w {
				cx = x
				y += ch + u.s(8)
			}
			b := box{cx, y, cw, ch}
			id := "season:" + strconv.Itoa(s.season)
			u.add(b, id, c.seasonsOn && !s.locked)
			switch {
			case s.on:
				bg := uint32(cGoldTint)
				if u.hot(id) && !s.locked {
					bg = 0x352C17
				}
				u.framed(b, u.s(8), bg, cGoldEdge)
				u.tick3(box{cx + u.s(9), y + (ch-u.s(16))/2, u.s(16), u.s(16)}, cGold)
				u.line(box{cx + u.s(30), y, tw + u.s(4), ch}, s.label, "bold", cGold)
			default:
				bg := uint32(cBg)
				if u.hot(id) {
					bg = cPanel
				}
				u.framed(b, u.s(8), bg, cEdge)
				u.line(box{cx + u.s(14), y, tw + u.s(4), ch}, s.label, "bold", cMuted)
			}
			cx += cw + u.s(8)
		}
		y += ch + u.s(10)
		if len(c.grid) > 0 {
			y = u.modeGrid(c, x, y+u.s(14), w)
		}
		y += u.wrap(x, y, w, c.seasonsSub, "small", cMuted) + u.s(10)
	}
	for _, l := range c.info {
		y += u.wrap(x, y, w, l, "small", cMuted) + u.s(4)
	}
	return y
}

func (u *gui) modeGrid(c cardView, x, y, w int32) int32 {
	u.line(box{x, y, w, u.s(18)}, "Рейды", "small", cMuted)
	y += u.s(24)
	lw := u.s(96)
	gap := u.s(8)
	cols := int32(len(c.gridHeads))
	cw := (w - lw - gap*cols) / cols
	for i, h := range c.gridHeads {
		u.text(box{x + lw + int32(i)*(cw+gap), y, cw, u.s(18)}, h, "small", cMuted, dtCenter|dtVCenter|dtSingleLine)
	}
	y += u.s(22)
	ch := u.s(34)
	for _, r := range c.grid {
		u.line(box{x, y, lw - u.s(8), ch}, r.title, "bold", cText)
		for i, cell := range r.cells {
			b := box{x + lw + int32(i)*(cw+gap), y, cw, ch}
			if !cell.avail {
				u.text(b, "—", "small", cMuted, dtCenter|dtVCenter|dtSingleLine)
				continue
			}
			id := "mode:" + cell.id
			u.add(b, id, c.seasonsOn)
			if cell.on {
				bg := uint32(cGoldTint)
				if u.hot(id) {
					bg = 0x352C17
				}
				u.framed(b, u.s(8), bg, cGoldEdge)
				u.tick3(box{b.x + u.s(9), y + (ch-u.s(16))/2, u.s(16), u.s(16)}, cGold)
				u.text(box{b.x + u.s(28), y, cw - u.s(34), ch}, cell.label, "bold", cGold, dtLeft|dtVCenter|dtSingleLine|dtEndEll)
			} else {
				bg := uint32(cBg)
				if u.hot(id) {
					bg = cPanel
				}
				u.framed(b, u.s(8), bg, cEdge)
				u.text(b, cell.label, "bold", cMuted, dtCenter|dtVCenter|dtSingleLine|dtEndEll)
			}
		}
		y += ch + u.s(8)
	}
	return y + u.s(6)
}

func (u *gui) rhPanels(c cardView, x, y, w int32) int32 {
	u.line(box{x, y, w, u.s(18)}, "Канал", "small", cMuted)
	y += u.s(26)
	seg := box{x, y, u.s(300), u.s(36)}
	u.round(seg, u.s(8), cPanel)
	half := seg.w / 2
	for i, label := range []string{"Стабильный", "Бета"} {
		b := box{seg.x + int32(i)*half + u.s(3), seg.y + u.s(3), half - u.s(6), seg.h - u.s(6)}
		id := "beta:" + strconv.Itoa(i)
		sel := c.beta == (i == 1)
		u.add(b, id, c.channelOn && !sel)
		switch {
		case sel:
			u.round(b, u.s(6), cGold)
			u.text(b, label, "btn", cOnGold, dtCenter|dtVCenter|dtSingleLine)
		case u.hot(id) && c.channelOn:
			u.round(b, u.s(6), cPanelHi)
			u.text(b, label, "btn", cBright, dtCenter|dtVCenter|dtSingleLine)
		default:
			u.text(b, label, "btn", cMuted, dtCenter|dtVCenter|dtSingleLine)
		}
	}
	hint := "бета — новые версии раньше всех"
	u.line(box{seg.x + seg.w + u.s(16), y, w - seg.w - u.s(16), seg.h}, hint, "small", cMuted)
	y += seg.h + u.s(24)
	rows := int32(len(c.packs))
	top := u.s(62)
	_, dh := u.size("Без залов реплей рисует плоский пол. Залы грузятся только при открытии реплея.", "small", w-u.s(200))
	top = u.s(42) + dh + u.s(12)
	ph := top + rows*u.s(36) + u.s(12)
	u.round(box{x, y, w, ph}, u.s(10), cPanel)
	u.line(box{x + u.s(20), y + u.s(14), w - u.s(200), u.s(22)}, "3D-залы реплея", "head", cBright)
	u.text(box{x + u.s(20), y + u.s(40), w - u.s(200), dh + u.s(2)}, "Без залов реплей рисует плоский пол. Залы грузятся только при открытии реплея.", "small", cMuted, dtLeft|dtWordBreak)
	bw := u.btnWidth("Применить")
	u.button(box{x + w - u.s(20) - bw, y + u.s(16), bw, u.s(36)}, "Применить", "apply", c.applyOn, false)
	ry := y + top
	for _, p := range c.packs {
		row := box{x + u.s(12), ry, w - u.s(24), u.s(34)}
		id := "pack:" + p.code
		u.add(row, id, p.enabled)
		if u.hot(id) && p.enabled {
			u.round(row, u.s(6), cPanelHi)
		}
		cb := box{row.x + u.s(10), ry + (row.h-u.s(20))/2, u.s(20), u.s(20)}
		switch {
		case p.checked && p.enabled:
			u.round(cb, u.s(5), cGold)
			u.tick3(cb, cOnGold)
		case p.checked:
			u.round(cb, u.s(5), cGoldEdge)
			u.tick3(cb, cPanel)
		default:
			u.framed(cb, u.s(5), cBg, cEdge)
		}
		lc := uint32(cText)
		if !p.enabled {
			lc = cMuted
		}
		u.line(box{cb.x + u.s(32), ry, u.s(160), row.h}, p.label, "body", lc)
		u.text(box{row.x + u.s(200), ry, row.w - u.s(212), row.h}, p.state, "small", cMuted, dtRight|dtVCenter|dtSingleLine|dtEndEll)
		ry += u.s(36)
	}
	y += ph + u.s(16)
	bw = u.btnWidth("Тестовая запись…")
	u.button(box{x, y, bw, u.s(36)}, "Тестовая запись…", "test", c.testOn, false)
	u.line(box{x + bw + u.s(16), y, w - bw - u.s(16), u.s(36)}, "чужая запись рейда, чтобы открыть разбор без своей", "small", cMuted)
	return u.discordPanel(x, y+u.s(56), w)
}

const dcAbout = "Кнопка «В Discord» в сводке рейда, затем /reload — итог уходит картинкой в канал по вебхуку, пока открыто это окно или включено автообновление. Без вебхука картинка ложится в Screenshots\\" + discordPicDir + "."

func (u *gui) discordPanel(x, y, w int32) int32 {
	d := u.app.discordView()
	if !u.hookInit {
		u.hookInit = true
		u.hookText = d.hook
	}
	iw := w - u.s(40)
	ix := x + u.s(20)
	_, ah := u.size(dcAbout, "small", iw)
	abw := u.btnWidth("Отправить снова")
	sbw := u.btnWidth("Сохранить")
	sw := iw - abw - u.s(16)
	_, sh := u.size(d.status, "small", sw)
	rowH := max(sh, u.s(36))
	ph := u.s(42) + ah + u.s(14) + u.s(24) + u.s(36) + u.s(14) + rowH + u.s(14) + u.s(36) + u.s(16)
	u.round(box{x, y, w, ph}, u.s(10), cPanel)
	u.line(box{ix, y + u.s(14), iw, u.s(22)}, "Итоги рейда в Discord", "head", cBright)
	ty := y + u.s(42)
	u.text(box{ix, ty, iw, ah + u.s(2)}, dcAbout, "small", cMuted, dtLeft|dtWordBreak)
	ty += ah + u.s(14)
	u.line(box{ix, ty, iw, u.s(18)}, "Вебхук Discord", "small", cMuted)
	ty += u.s(24)
	fw := iw - sbw - u.s(12)
	u.hookField(box{ix, ty, fw, u.s(36)})
	u.button(box{ix + fw + u.s(12), ty, sbw, u.s(36)}, "Сохранить", "hooksave", d.saveOn, false)
	ty += u.s(36) + u.s(14)
	u.text(box{ix, ty + (rowH-sh)/2, sw, sh + u.s(2)}, d.status, "small", kindColor(d.kind), dtLeft|dtWordBreak)
	u.button(box{ix + iw - abw, ty + (rowH-u.s(36))/2, abw, u.s(36)}, "Отправить снова", "dcagain", d.againOn, false)
	ty += rowH + u.s(14)
	lbw := u.btnWidth(dcLogsBtn)
	u.button(box{ix, ty, lbw, u.s(36)}, dcLogsBtn, "logs", true, false)
	u.text(box{ix + lbw + u.s(16), ty, iw - lbw - u.s(16), u.s(36)}, dcLogsHint, "small", cMuted, dtLeft|dtVCenter|dtSingleLine|dtEndEll)
	return y + ph + u.s(4)
}

func (u *gui) hookField(b box) {
	u.add(b, "hook", true)
	edge := uint32(cEdge)
	switch {
	case u.hookFocus:
		edge = cGoldEdge
	case u.hot("hook"):
		edge = 0x4A4A54
	}
	u.framed(b, u.s(7), cBg, edge)
	in := box{b.x + u.s(12), b.y, b.w - u.s(24), b.h}
	if u.hookText == "" && !u.hookFocus {
		u.line(in, "https://discord.com/api/webhooks/…", "body", cMuted)
		return
	}
	r := []rune(maskHook(u.hookText))
	lead := ""
	for len(r) > 0 {
		if tw, _ := u.size(lead+string(r), "body", 0); tw <= in.w-u.s(4) {
			break
		}
		r, lead = r[1:], "…"
	}
	s := lead + string(r)
	u.line(in, s, "body", cText)
	if u.hookFocus {
		tw := int32(0)
		if s != "" {
			tw, _ = u.size(s, "body", 0)
		}
		u.fill(box{in.x + tw + u.s(1), b.y + u.s(9), max(u.s(1), 1), b.h - u.s(18)}, cGold)
	}
}

func (u *gui) saveHook() {
	u.hookText = strings.TrimSpace(u.hookText)
	if u.app.saveHook(u.hookText) {
		u.hookFocus = false
	}
}

func (u *gui) hookKey(c rune) {
	switch {
	case c == 0x08:
		if r := []rune(u.hookText); len(r) > 0 {
			u.hookText = string(r[:len(r)-1])
		}
	case c == 0x7F:
		u.hookText = ""
	case c == 0x16:
		u.hookText += strings.Join(strings.Fields(u.clipText()), "")
	case c >= 0x20 && (c < 0xD800 || c > 0xDFFF):
		u.hookText += string(c)
	}
	if r := []rune(u.hookText); len(r) > 512 {
		u.hookText = string(r[:512])
	}
}

func (u *gui) clipText() string {
	if r, _, _ := uOpenClipboard.Call(u.hwnd); r == 0 {
		return ""
	}
	defer uCloseClipboard.Call()
	h, _, _ := uGetClipboardData.Call(13)
	if h == 0 {
		return ""
	}
	p, _, _ := kGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer kGlobalUnlock.Call(h)
	return windows.UTF16PtrToString(*(**uint16)(unsafe.Pointer(&p)))
}

func (u *gui) drawWelcome(x, y, w int32) int32 {
	u.line(box{x, y, w, u.s(34)}, "Где игра?", "title", cBright)
	y += u.s(44)
	y += u.wrap(x, y, w, "Не нашёл папку игры рядом с программой. Положите ManacodeUpdate.exe в папку любого аддона Manacode в Interface\\AddOns или укажите папку игры — дальше программа запомнит её.", "body", cText) + u.s(20)
	bw := u.btnWidth("Указать папку игры")
	u.button(box{x, y, bw, u.s(38)}, "Указать папку игры", "folder", true, true)
	return y + u.s(48)
}

func (u *gui) drawTest(x, y, w int32) int32 {
	t := u.app.testView()
	lw, _ := u.size("← Raid Helper", "body", 0)
	back := box{x, y, lw, u.s(22)}
	u.add(back, "back", !t.busy)
	bc := uint32(cGold)
	if u.hot("back") {
		bc = cGoldHi
	}
	u.line(back, "← Raid Helper", "body", bc)
	y += u.s(30)
	u.line(box{x, y, w, u.s(34)}, "Тестовая запись", "title", cBright)
	y += u.s(40)
	y += u.wrap(x, y, w, "Чужая запись рейда, чтобы открыть разбор без своей. Своя уходит в копию и возвращается кнопкой «Вернуть свою». Игра должна быть закрыта.", "body", cMuted) + u.s(18)
	u.line(box{x, y, w, u.s(18)}, "Запись", "small", cMuted)
	y += u.s(24)
	for i, s := range t.sets {
		y = u.rowItem(box{x, y, w, u.s(36)}, "set:"+strconv.Itoa(i), s, "", i == t.set, !t.busy)
	}
	setKind := kindNone
	if t.sets != nil {
		setKind = kindErr
		if t.setOK {
			setKind = kindOK
		}
	}
	y += u.wrap(x, y+u.s(2), w, t.setInfo, "small", kindColor(setKind)) + u.s(18)
	u.line(box{x, y, w, u.s(18)}, "Аккаунт", "small", cMuted)
	y += u.s(24)
	for i, a := range t.accs {
		name, state, _ := strings.Cut(a, "\t")
		y = u.rowItem(box{x, y, w, u.s(36)}, "acc:"+strconv.Itoa(i), name, state, i == t.acc, !t.busy)
	}
	y += u.s(10)
	bw := u.btnWidth("Поставить запись")
	u.button(box{x, y, bw, u.s(38)}, "Поставить запись", "tinstall", t.installOn, true)
	rw := u.btnWidth("Вернуть свою")
	u.button(box{x + bw + u.s(12), y, rw, u.s(38)}, "Вернуть свою", "trestore", t.restoreOn, false)
	y += u.s(50)
	if t.busy {
		u.progress(box{x, y, w, u.s(6)}, t.got, t.total)
		y += u.s(16)
	}
	if t.status != "" && (t.kind != kindNone || t.busy) {
		y = u.notice(x, y, w, t.status, t.kind)
	}
	return y
}

func (u *gui) rowItem(b box, id, title, sub string, sel, on bool) int32 {
	u.add(b, id, on && !sel)
	switch {
	case sel:
		u.framed(b, u.s(8), cGoldBg, cGoldEdge)
	case u.hot(id) && on:
		u.round(b, u.s(8), cPanelHi)
	default:
		u.round(b, u.s(8), cPanel)
	}
	tc := uint32(cText)
	if sel {
		tc = cGold
	}
	tw := b.w - u.s(32)
	if sub != "" {
		tw = b.w * 45 / 100
	}
	u.line(box{b.x + u.s(16), b.y, tw, b.h}, title, "bold", tc)
	if sub != "" {
		u.text(box{b.x + b.w*45/100 + u.s(24), b.y, b.w*55/100 - u.s(40), b.h}, sub, "small", cMuted, dtRight|dtVCenter|dtSingleLine|dtEndEll)
	}
	return b.y + b.h + u.s(6)
}

func (u *gui) render(dc uintptr) {
	var bits unsafe.Pointer
	bi := bmpInfo{width: u.w, height: -u.h, planes: 1, bitCount: 32}
	bi.size = 40
	mem, _, _ := gCreateCompatibleDC.Call(dc)
	bmp, _, _ := gCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	old, _, _ := gSelectObject.Call(mem, bmp)
	pCreateFromHDC.Call(mem, uintptr(unsafe.Pointer(&u.g)))
	pSetSmoothing.Call(u.g, 4)
	pSetPixelOffset.Call(u.g, 4)
	u.dc = mem
	u.draw()
	pDeleteGraphics.Call(u.g)
	u.g = 0
	if u.shot != "" && u.armed && dc == 0 {
		u.savePNG(bits)
	} else if dc != 0 {
		gBitBlt.Call(dc, 0, 0, uintptr(u.w), uintptr(u.h), mem, 0, 0, 0x00CC0020)
	}
	gSelectObject.Call(mem, old)
	gDeleteObject.Call(bmp)
	gDeleteDC.Call(mem)
}

func (u *gui) savePNG(bits unsafe.Pointer) {
	n := int(u.w) * int(u.h) * 4
	src := unsafe.Slice((*byte)(bits), n)
	img := image.NewRGBA(image.Rect(0, 0, int(u.w), int(u.h)))
	for i := 0; i+3 < n; i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	if f, err := os.Create(u.shot); err == nil {
		_ = png.Encode(f, img)
		f.Close()
	}
}

func (u *gui) invalidate() {
	uInvalidateRect.Call(u.hwnd, 0, 0)
}

func (u *gui) at(lp uintptr) (int32, int32) {
	return int32(int16(lp & 0xFFFF)), int32(int16(lp >> 16 & 0xFFFF))
}

func (u *gui) hitAt(x, y int32) (hit, bool) {
	for i := len(u.hits) - 1; i >= 0; i-- {
		if u.hits[i].b.has(x, y) {
			return u.hits[i], true
		}
	}
	return hit{}, false
}

func (u *gui) click(id string) {
	a := u.app
	key, arg, _ := strings.Cut(id, ":")
	switch key {
	case "card":
		i, _ := strconv.Atoi(arg)
		u.scroll = 0
		a.pick(i)
	case "act":
		a.mainAction(arg)
	case "data":
		a.dataAction()
	case "season":
		s, _ := strconv.Atoi(arg)
		a.toggleSeason(s)
	case "mode":
		a.toggleMode(arg)
	case "beta":
		a.setBeta(arg == "1")
	case "pack":
		a.togglePack(arg)
	case "apply":
		a.applyPacks()
	case "test":
		u.scroll = 0
		a.openTest()
	case "back":
		u.scroll = 0
		a.closeTest()
	case "set":
		i, _ := strconv.Atoi(arg)
		a.pickTest(i, -1)
	case "acc":
		i, _ := strconv.Atoi(arg)
		a.pickTest(-1, i)
	case "tinstall":
		a.testJob(true)
	case "trestore":
		a.testJob(false)
	case "hook":
		u.hookFocus = true
	case "hooksave":
		u.saveHook()
	case "dcagain":
		a.discordAgain()
	case "logs":
		openLogs()
	case "auto":
		a.setAuto(!a.view().auto)
	case "hours":
		a.toggleHours()
	case "folder":
		u.pickFolder()
	case "self":
		if page := a.selfPage(); strings.HasPrefix(page, "https://") {
			sShellExecute.Call(u.hwnd, uintptr(unsafe.Pointer(u16p("open"))), uintptr(unsafe.Pointer(u16p(page))), 0, 0, 1)
		}
	case "open":
		key, what, _ := strings.Cut(arg, ":")
		for _, c := range a.view().cards {
			if c.key != key {
				continue
			}
			target := c.folder
			switch what {
			case "page":
				target = c.page
			case "curse":
				target = c.curse
			}
			if target != "" && (what == "folder" || strings.HasPrefix(target, "https://")) {
				sShellExecute.Call(u.hwnd, uintptr(unsafe.Pointer(u16p("open"))), uintptr(unsafe.Pointer(u16p(target))), 0, 0, 1)
			}
		}
	}
	u.invalidate()
}

func (u *gui) pickFolder() {
	bi := browseInfoW{owner: u.hwnd, title: u16p("Папка игры WoW — та, где лежит Wow.exe"), flags: 0x1 | 0x40}
	id, _, _ := sBrowseForFolder.Call(uintptr(unsafe.Pointer(&bi)))
	if id == 0 {
		return
	}
	defer oCoTaskMemFree.Call(id)
	buf := make([]uint16, 1024)
	if r, _, _ := sPathFromIDList.Call(id, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return
	}
	addons := pickedAddOns(syscall.UTF16ToString(buf))
	if addons == "" {
		u.message("В этой папке нет игры: выберите папку, где лежит Wow.exe.")
		return
	}
	u.app.choose(addons)
}

func (u *gui) message(text string) {
	uMessageBox.Call(u.hwnd, uintptr(unsafe.Pointer(u16p(text))), uintptr(unsafe.Pointer(u16p("Manacode"))), 0x40)
}

func (u *gui) layoutSize() (int32, int32) {
	w, h := u.s(940), u.s(640)
	var wa winRect
	if r, _, _ := uSysParamsInfo.Call(0x30, 0, uintptr(unsafe.Pointer(&wa)), 0); r != 0 {
		w = min(w, wa.right-wa.left-u.s(40))
		h = min(h, wa.bottom-wa.top-u.s(60))
	}
	return w, h
}

func (u *gui) darkFrame() {
	on := int32(1)
	dSetWindowAttr.Call(u.hwnd, 20, uintptr(unsafe.Pointer(&on)), 4)
	cap := uint32(colorref(cSide))
	dSetWindowAttr.Call(u.hwnd, 35, uintptr(unsafe.Pointer(&cap)), 4)
	txt := uint32(colorref(cText))
	dSetWindowAttr.Call(u.hwnd, 36, uintptr(unsafe.Pointer(&txt)), 4)
}

func (u *gui) shotReady() bool {
	a := u.app
	a.mu.Lock()
	ready := true
	for _, c := range a.cards {
		if a.addons != "" && (c.loading || c.rel == nil || c.idxBusy) {
			ready = false
		}
	}
	t := a.test
	a.mu.Unlock()
	if u.shotTest {
		if ready && t == nil {
			a.openTest()
			return false
		}
		ready = ready && t != nil && !t.loading
	}
	return ready || time.Since(u.started) > 25*time.Second
}

func runGUI(addons string, shot, shotKey string, shotTest bool) int {
	if shot == "" && !takeGUI() {
		return 0
	}
	if uSetDPIContext.Find() == nil {
		uSetDPIContext.Call(^uintptr(3))
	} else {
		uSetDPIAware.Call()
	}
	oCoInitializeEx.Call(0, 2)
	var token uintptr
	in := gdipInput{version: 1}
	pStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&in)), 0)
	inst, _, _ := kGetModuleHandle.Call(0)
	u := &gui{inst: inst, shot: shot, shotKey: shotKey, shotTest: shotTest, started: time.Now()}
	theGUI = u
	dc, _, _ := uGetDC.Call(0)
	d, _, _ := gGetDeviceCaps.Call(dc, 90)
	uReleaseDC.Call(0, dc)
	u.dpi = max(int32(d), 96)
	u.measure, _, _ = gCreateCompatibleDC.Call(0)
	u.makeFonts()
	cursor, _, _ := uLoadCursor.Call(0, 32512)
	icon, _, _ := uLoadIcon.Call(inst, 1)
	if icon == 0 {
		icon, _, _ = uLoadIcon.Call(0, 32512)
	}
	sx, _, _ := uGetSysMetrics.Call(49)
	sy, _, _ := uGetSysMetrics.Call(50)
	small, _, _ := uLoadImage.Call(inst, 1, 1, sx, sy, 0)
	if small == 0 {
		small = icon
	}
	u.loadLogo()
	wc := wndClassEx{style: csHRedraw | csVRedraw, proc: syscall.NewCallback(guiProc), inst: inst, icon: icon, cursor: cursor, name: u16p(guiClass), iconSm: small}
	wc.size = uint32(unsafe.Sizeof(wc))
	uRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	cw, ch := u.layoutSize()
	r := winRect{right: cw, bottom: ch}
	uAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedWindow, 0, 0)
	ww, wh := r.right-r.left, r.bottom-r.top
	var wa winRect
	uSysParamsInfo.Call(0x30, 0, uintptr(unsafe.Pointer(&wa)), 0)
	x, y := wa.left+(wa.right-wa.left-ww)/2, wa.top+(wa.bottom-wa.top-wh)/2
	u.hwnd, _, _ = uCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16p(guiClass))), uintptr(unsafe.Pointer(u16p(guiTitle()))),
		wsOverlappedWindow, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0, 0, inst, 0)
	if uGetDpiForWindow.Find() == nil {
		if d, _, _ := uGetDpiForWindow.Call(u.hwnd); d != 0 && int32(d) != u.dpi {
			u.dpi = int32(d)
			u.makeFonts()
		}
	}
	u.darkFrame()
	u.w, u.h = cw, ch
	u.app = newApp(addons, func() { uPostMessage.Call(u.hwnd, wmApp, 0, 0) })
	if shotKey != "" {
		for i, d := range catalog {
			if d.key == shotKey {
				u.app.cur = i
			}
		}
	}
	u.app.game = gameRunning()
	if shot == "" {
		uShowWindow.Call(u.hwnd, 5)
		uSetForeground.Call(u.hwnd)
	}
	uSetTimer.Call(u.hwnd, 1, 60, 0)
	uSetTimer.Call(u.hwnd, 2, 2000, 0)
	u.app.refresh()
	if shot == "" {
		uSetTimer.Call(u.hwnd, 3, 5000, 0)
		u.app.discordPoll()
	}
	var m winMsg
	for {
		r, _, _ := uGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if r == 0 || r == ^uintptr(0) {
			break
		}
		uTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		uDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	return 0
}

func (u *gui) busyNow() bool {
	v := u.app.view()
	if v.busy {
		return true
	}
	if v.page == pageTest {
		return u.app.testView().busy
	}
	return false
}

func guiProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	u := theGUI
	switch msg {
	case wmApp:
		if v := u.app.view(); v.restart && !u.restarting {
			u.restarting = true
			guiUnlock()
			if exe, err := exePath(); err == nil && hop(exe, nil) {
				uPostQuitMessage.Call(0)
				return 0
			}
			if unlock, ok := lockWatch(guiMutex); ok {
				guiUnlock = unlock
			}
		}
		u.invalidate()
		return 0
	case wmCopyData:
		if s, ok := copyText(lParam); ok {
			u.app.setNotice(s)
			uSetForeground.Call(hwnd)
			return 1
		}
		return 0
	case wmEraseBkgnd:
		return 1
	case wmPaint:
		var ps paintStruct
		dc, _, _ := uBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		u.render(dc)
		uEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmSize:
		var r winRect
		uGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		if r.right > 0 && r.bottom > 0 {
			u.w, u.h = r.right, r.bottom
		}
		u.invalidate()
		return 0
	case wmGetMinMaxInfo:
		mm := *(**minMaxInfo)(unsafe.Pointer(&lParam))
		mm.minTrack = winPoint{u.s(820), u.s(520)}
		return 0
	case wmDpiChanged:
		u.dpi = int32(wParam & 0xFFFF)
		u.makeFonts()
		r := *(**winRect)(unsafe.Pointer(&lParam))
		uSetWindowPos.Call(hwnd, 0, uintptr(r.left), uintptr(r.top), uintptr(r.right-r.left), uintptr(r.bottom-r.top), 0x14)
		u.invalidate()
		return 0
	case wmTimer:
		if wParam == 2 {
			if u.app.setGame(gameRunning()) {
				u.invalidate()
			}
			return 0
		}
		if wParam == 3 {
			u.app.discordPoll()
			return 0
		}
		u.tick++
		if u.shot != "" {
			if !u.armed && u.shotReady() && time.Since(u.started) > time.Second {
				u.armed = true
				u.render(0)
				uPostQuitMessage.Call(0)
			}
			return 0
		}
		if u.busyNow() {
			u.invalidate()
		}
		return 0
	case wmMouseMove:
		if !u.tracked {
			tm := trackMouse{flags: 0x2, hwnd: hwnd}
			tm.size = uint32(unsafe.Sizeof(tm))
			uTrackMouse.Call(uintptr(unsafe.Pointer(&tm)))
			u.tracked = true
		}
		x, y := u.at(lParam)
		id := ""
		if h, ok := u.hitAt(x, y); ok && h.on {
			id = h.id
		}
		if id != u.hover {
			u.hover = id
			u.invalidate()
		}
		return 0
	case wmMouseLeave:
		u.tracked = false
		if u.hover != "" {
			u.hover = ""
			u.invalidate()
		}
		return 0
	case wmSetCursor:
		if lParam&0xFFFF == 1 && u.hover != "" {
			shape := uintptr(32649)
			if u.hover == "hook" {
				shape = 32513
			}
			c, _, _ := uLoadCursor.Call(0, shape)
			uSetCursor.Call(c)
			return 1
		}
	case wmLButtonDown:
		x, y := u.at(lParam)
		u.press = ""
		if h, ok := u.hitAt(x, y); ok && h.on {
			u.press = h.id
		}
		if u.hookFocus && u.press != "hook" && u.press != "hooksave" {
			u.hookFocus = false
			u.invalidate()
		}
		return 0
	case wmLButtonUp:
		x, y := u.at(lParam)
		if h, ok := u.hitAt(x, y); ok && h.on && h.id == u.press {
			u.click(h.id)
		}
		u.press = ""
		return 0
	case wmMouseWheel:
		delta := int32(int16(wParam >> 16 & 0xFFFF))
		u.scroll -= delta * u.s(48) / 120
		u.scroll = max(0, min(u.scroll, max(u.content-u.view, 0)))
		u.invalidate()
		return 0
	case wmChar:
		if u.hookFocus {
			u.hookKey(rune(wParam))
			u.invalidate()
		}
		return 0
	case wmKeyDown:
		if u.hookFocus {
			switch wParam {
			case vkReturn:
				u.saveHook()
			case vkEscape:
				u.hookFocus = false
			case vkF5:
				u.app.refresh()
			}
			u.invalidate()
			return 0
		}
		switch wParam {
		case vkUp:
			u.scroll = 0
			u.app.step(-1)
		case vkDown:
			u.scroll = 0
			u.app.step(1)
		case vkReturn:
			u.app.primaryAction()
		case vkF5:
			u.app.refresh()
		case vkEscape:
			u.app.closeTest()
		}
		u.invalidate()
		return 0
	case wmClose:
		if u.busyNow() {
			u.message("Идёт установка — дождитесь конца, потом закройте окно.")
			return 0
		}
	case wmDestroy:
		uPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := uDefWindowProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}
