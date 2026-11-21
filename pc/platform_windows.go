//go:build windows

package main

import (
	"log"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	imm32    = syscall.NewLazyDLL("imm32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	shcore   = syscall.NewLazyDLL("shcore.dll")
)

func syscallLazy(name string) *syscall.LazyDLL { return syscall.NewLazyDLL(name) }

var (
	pSendInput                     = user32.NewProc("SendInput")
	pMapVirtualKeyW                = user32.NewProc("MapVirtualKeyW")
	pVkKeyScanExW                  = user32.NewProc("VkKeyScanExW")
	pGetKeyboardLayout             = user32.NewProc("GetKeyboardLayout")
	pGetKeyState                   = user32.NewProc("GetKeyState")
	pGetForegroundWindow           = user32.NewProc("GetForegroundWindow")
	pGetWindowThreadProcessId      = user32.NewProc("GetWindowThreadProcessId")
	pGetGUIThreadInfo              = user32.NewProc("GetGUIThreadInfo")
	pSendMessageTimeoutW           = user32.NewProc("SendMessageTimeoutW")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pImmGetDefaultIMEWnd           = imm32.NewProc("ImmGetDefaultIMEWnd")
	pCreateMutexW                  = kernel32.NewProc("CreateMutexW")
	pShellExecuteW                 = shell32.NewProc("ShellExecuteW")
)

const (
	inputMouseType    = 0
	inputKeyboardType = 1

	keyeventfExtended = 0x0001
	keyeventfKeyUp    = 0x0002
	keyeventfUnicode  = 0x0004

	mouseMove       = 0x0001
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000

	wmImeControl         = 0x0283
	imcGetConversionMode = 0x0001
	imcSetConversionMode = 0x0002
	imcGetOpenStatus     = 0x0005
	imcSetOpenStatus     = 0x0006
	imeCmodeNative       = 0x0001
	imeCmodeFullShape    = 0x0008
	imeCmodeRoman        = 0x0010
	smtoAbortIfHung      = 0x0002
	errorAlreadyExists   = 183
)

// INPUT 構造体（x64: 40バイト）
type mouseInput struct {
	Dx, Dy      int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type keybdInput struct {
	WVk, WScan  uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
	_           [8]byte
}

type inputM struct {
	Type uint32
	_    uint32
	Mi   mouseInput
}

type inputK struct {
	Type uint32
	_    uint32
	Ki   keybdInput
}

type winInjector struct{}

func NewInjector() Injector { return &winInjector{} }

func isExtended(vk uint16) bool {
	switch vk {
	case 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28, 0x2C, 0x2D, 0x2E, 0x5B, 0x5C, 0x5D, 0x6F, 0x90, 0xA3, 0xA5:
		return true
	}
	return false
}

func keyEv(vk uint16, up bool) inputK {
	scan, _, _ := pMapVirtualKeyW.Call(uintptr(vk), 0)
	var f uint32
	if isExtended(vk) {
		f |= keyeventfExtended
	}
	if up {
		f |= keyeventfKeyUp
	}
	return inputK{Type: inputKeyboardType, Ki: keybdInput{WVk: vk, WScan: uint16(scan), DwFlags: f}}
}

func sendKeys(in []inputK) {
	if len(in) == 0 {
		return
	}
	n, _, err := pSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
	if int(n) != len(in) {
		log.Printf("SendInput(keys) sent %d/%d: %v", n, len(in), err)
	}
}

func sendMouse(mi mouseInput) {
	in := inputM{Type: inputMouseType, Mi: mi}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func (w *winInjector) KeyCombo(mods []uint16, vk uint16) {
	var in []inputK
	for _, m := range mods {
		in = append(in, keyEv(m, false))
	}
	in = append(in, keyEv(vk, false), keyEv(vk, true))
	for i := len(mods) - 1; i >= 0; i-- {
		in = append(in, keyEv(mods[i], true))
	}
	sendKeys(in)
}

func (w *winInjector) KeyDown(vk uint16) { sendKeys([]inputK{keyEv(vk, false)}) }
func (w *winInjector) KeyUp(vk uint16)   { sendKeys([]inputK{keyEv(vk, true)}) }

func foregroundHKL() uintptr {
	fg, _, _ := pGetForegroundWindow.Call()
	var tid uintptr
	if fg != 0 {
		tid, _, _ = pGetWindowThreadProcessId.Call(fg, 0)
	}
	hkl, _, _ := pGetKeyboardLayout.Call(tid)
	return hkl
}

func unicodeEvents(r rune) []inputK {
	var in []inputK
	for _, u := range utf16.Encode([]rune{r}) {
		in = append(in,
			inputK{Type: inputKeyboardType, Ki: keybdInput{WScan: u, DwFlags: keyeventfUnicode}},
			inputK{Type: inputKeyboardType, Ki: keybdInput{WScan: u, DwFlags: keyeventfUnicode | keyeventfKeyUp}})
	}
	return in
}

// TypeASCII は現在のキーボード配列(JIS/US等)に合わせて仮想キーで入力する。
// IMEがONならローマ字入力として変換される。
func (w *winInjector) TypeASCII(s string) {
	hkl := foregroundHKL()
	caps, _, _ := pGetKeyState.Call(0x14)
	capsOn := caps&1 != 0
	var in []inputK
	for _, r := range s {
		res, _, _ := pVkKeyScanExW.Call(uintptr(uint16(r)), hkl)
		v := int16(res)
		if v == -1 || (v>>8)&6 != 0 {
			in = append(in, unicodeEvents(r)...)
			continue
		}
		vk := uint16(v & 0xff)
		shift := (v>>8)&1 != 0
		if capsOn && ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			shift = !shift
		}
		if shift {
			in = append(in, keyEv(vkShift, false))
		}
		in = append(in, keyEv(vk, false), keyEv(vk, true))
		if shift {
			in = append(in, keyEv(vkShift, true))
		}
	}
	sendKeys(in)
}

func (w *winInjector) TypeUnicode(r rune) { sendKeys(unicodeEvents(r)) }

func (w *winInjector) MouseMove(dx, dy int) {
	sendMouse(mouseInput{Dx: int32(dx), Dy: int32(dy), DwFlags: mouseMove})
}

func (w *winInjector) MouseButton(b int, down bool) {
	flags := [3][2]uint32{{mouseLeftUp, mouseLeftDown}, {mouseRightUp, mouseRightDown}, {mouseMiddleUp, mouseMiddleDown}}
	if b < 0 || b > 2 {
		return
	}
	d := 0
	if down {
		d = 1
	}
	sendMouse(mouseInput{DwFlags: flags[b][d]})
}

func (w *winInjector) Wheel(delta int, horizontal bool) {
	f := uint32(mouseWheel)
	if horizontal {
		f = mouseHWheel
	}
	sendMouse(mouseInput{MouseData: uint32(int32(delta)), DwFlags: f})
}

type guiThreadInfo struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       [4]int32
}

func imeMsg(wnd uintptr, cmd, val uintptr) (uintptr, bool) {
	var res uintptr
	r, _, _ := pSendMessageTimeoutW.Call(wnd, wmImeControl, cmd, val, smtoAbortIfHung, 150, uintptr(unsafe.Pointer(&res)))
	return res, r != 0
}

// SetIME は前面ウィンドウのIMEをON/OFFする。
// 変換モード（ひらがな/ローマ字など）には触れない（触ると入力中の文字が確定されることがある）。
func (w *winInjector) Foreground() uintptr {
	h, _, _ := pGetForegroundWindow.Call()
	return h
}

func (w *winInjector) SetIME(open bool, kana bool) {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg == 0 {
		return
	}
	target := fg
	tid, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
	gti := guiThreadInfo{CbSize: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	if r, _, _ := pGetGUIThreadInfo.Call(tid, uintptr(unsafe.Pointer(&gti))); r != 0 && gti.HwndFocus != 0 {
		target = gti.HwndFocus
	}
	ime, _, _ := pImmGetDefaultIMEWnd.Call(target)
	if ime == 0 {
		return
	}
	cur, ok := imeMsg(ime, imcGetOpenStatus, 0)
	if !ok {
		return
	}
	if open != (cur != 0) {
		v := uintptr(0)
		if open {
			v = 1
		}
		imeMsg(ime, imcSetOpenStatus, v)
	}
}

// IMEOpen は前面のアプリでIMEがONか（問い合わせのみ。状態は変えない）。
func (w *winInjector) IMEOpen() bool {
	fg, _, _ := pGetForegroundWindow.Call()
	if fg == 0 {
		return false
	}
	target := fg
	tid, _, _ := pGetWindowThreadProcessId.Call(fg, 0)
	gti := guiThreadInfo{CbSize: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	if r, _, _ := pGetGUIThreadInfo.Call(tid, uintptr(unsafe.Pointer(&gti))); r != 0 && gti.HwndFocus != 0 {
		target = gti.HwndFocus
	}
	ime, _, _ := pImmGetDefaultIMEWnd.Call(target)
	if ime == 0 {
		return false
	}
	cur, ok := imeMsg(ime, imcGetOpenStatus, 0)
	return ok && cur != 0
}

var instanceMutex uintptr

func acquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString("Local\\PhoneBoardPC_SingleInstance")
	h, _, err := pCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	instanceMutex = h
	if errno, ok := err.(syscall.Errno); ok && errno == errorAlreadyExists {
		return false
	}
	return true
}

func initPlatform() {
	// Per-Monitor V2 DPI 対応（文字がぼやけないように）
	if pSetProcessDpiAwarenessContext.Find() == nil {
		r, _, _ := pSetProcessDpiAwarenessContext.Call(^uintptr(3)) // -4
		if r != 0 {
			return
		}
	}
	if pSetProcessDPIAware.Find() == nil {
		pSetProcessDPIAware.Call()
	}
}

func openBrowser(url string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	u, _ := syscall.UTF16PtrFromString(url)
	pShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(u)), 0, 0, 1)
}
