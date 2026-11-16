//go:build windows

package main

import (
	"log"
	"math"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	pRegisterClassExW       = user32.NewProc("RegisterClassExW")
	pCreateWindowExW        = user32.NewProc("CreateWindowExW")
	pDefWindowProcW         = user32.NewProc("DefWindowProcW")
	pGetMessageW            = user32.NewProc("GetMessageW")
	pTranslateMessage       = user32.NewProc("TranslateMessage")
	pDispatchMessageW       = user32.NewProc("DispatchMessageW")
	pPostQuitMessage        = user32.NewProc("PostQuitMessage")
	pShowWindow             = user32.NewProc("ShowWindow")
	pSetWindowPos           = user32.NewProc("SetWindowPos")
	pSetLayeredWindowAttrs  = user32.NewProc("SetLayeredWindowAttributes")
	pSetTimer               = user32.NewProc("SetTimer")
	pInvalidateRect         = user32.NewProc("InvalidateRect")
	pBeginPaint             = user32.NewProc("BeginPaint")
	pEndPaint               = user32.NewProc("EndPaint")
	pGetClientRect          = user32.NewProc("GetClientRect")
	pFillRect               = user32.NewProc("FillRect")
	pDrawTextW              = user32.NewProc("DrawTextW")
	pLoadCursorW            = user32.NewProc("LoadCursorW")
	pLoadIconW              = user32.NewProc("LoadIconW")
	pMonitorFromWindow      = user32.NewProc("MonitorFromWindow")
	pGetMonitorInfoW        = user32.NewProc("GetMonitorInfoW")
	pGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
	pCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	pAppendMenuW            = user32.NewProc("AppendMenuW")
	pTrackPopupMenu         = user32.NewProc("TrackPopupMenu")
	pDestroyMenu            = user32.NewProc("DestroyMenu")
	pGetCursorPos           = user32.NewProc("GetCursorPos")
	pSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	pRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	pPostMessageW           = user32.NewProc("PostMessageW")
	pGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	pShellNotifyIconW       = shell32.NewProc("Shell_NotifyIconW")
	pGetDpiForMonitor       = shcore.NewProc("GetDpiForMonitor")
	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pCreatePen              = gdi32.NewProc("CreatePen")
	pGetStockObject         = gdi32.NewProc("GetStockObject")
	pRoundRect              = gdi32.NewProc("RoundRect")
	pEllipse                = gdi32.NewProc("Ellipse")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pCreateFontW            = gdi32.NewProc("CreateFontW")
)

const (
	wsPopup         = 0x80000000
	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000
	swHide          = 0
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoActivate   = 0x0010
	swpShowWindow   = 0x0040
	lwaAlpha        = 0x2
	lwaColorKey     = 0x1
	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmMouseActivate = 0x0021
	wmNcHitTest     = 0x0084
	wmTimer         = 0x0113
	wmApp           = 0x8000
	wmTray          = wmApp + 1
	wmLButtonUp     = 0x0202
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B
	nimAdd          = 0
	nimModify       = 1
	nimDelete       = 2
	nifMessage      = 1
	nifIcon         = 2
	nifTip          = 4
	srccopy         = 0x00CC0020
	dtCenter        = 0x01
	dtVCenter       = 0x04
	dtSingleLine    = 0x20
	dtNoPrefix      = 0x800
	dtEndEllipsis   = 0x8000
	dtLeft          = 0x00
	dtRight         = 0x02
	hwndTopmost     = ^uintptr(0)
)

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type msgT struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       [2]int32
	LPrivate uint32
}

type rect struct{ L, T, R, B int32 }

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// オーバーレイ用の小さなウィンドウ
type ovWin struct {
	hwnd    uintptr
	shown   bool
	x, y    int32
	w, h    int32
	paint   func(c *canvas, w, h int32)
	lastTop time.Time
}

type ui struct {
	app       *App
	hinst     uintptr
	main      uintptr
	kb, mini  *ovWin
	toast     *ovWin
	data      OverlayData
	ver       uint64
	scale     float64
	fonts     map[[2]int]uintptr
	nid       notifyIconData
	tbCreated uintptr
	tipConn   bool
	wins      map[uintptr]*ovWin
}

var theUI *ui

func u16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func RunUI(app *App) {
	runtime.LockOSThread()
	u := &ui{app: app, fonts: map[[2]int]uintptr{}, wins: map[uintptr]*ovWin{}, scale: 1}
	theUI = u
	u.hinst, _, _ = pGetModuleHandleW.Call(0)
	cb := syscall.NewCallback(wndProc)
	cursor, _, _ := pLoadCursorW.Call(0, 32512)

	for _, name := range []string{"PhoneBoardOverlay", "PhoneBoardMain"} {
		wc := wndClassEx{LpfnWndProc: cb, HInstance: u.hinst, HCursor: cursor, LpszClassName: u16(name)}
		wc.CbSize = uint32(unsafe.Sizeof(wc))
		if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			log.Printf("RegisterClassEx %s: %v", name, err)
		}
	}
	u.main, _, _ = pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(u16("PhoneBoardMain"))),
		uintptr(unsafe.Pointer(u16("PhoneBoard"))), 0, 0, 0, 0, 0, 0, 0, u.hinst, 0)
	u.kb = u.newOverlay("PhoneBoard Keyboard", u.paintKb)
	u.mini = u.newOverlay("PhoneBoard Mouse", u.paintMini)
	u.toast = u.newOverlay("PhoneBoard Toast", u.paintToast)

	u.tbCreated, _, _ = pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
	u.addTray()
	pSetTimer.Call(u.main, 1, 16, 0)

	var m msgT
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&u.nid)))
	os.Exit(0)
}

func (u *ui) newOverlay(title string, paint func(c *canvas, w, h int32)) *ovWin {
	ex := uintptr(wsExTopmost | wsExTransparent | wsExToolWindow | wsExLayered | wsExNoActivate)
	h, _, err := pCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(u16("PhoneBoardOverlay"))),
		uintptr(unsafe.Pointer(u16(title))), wsPopup, 0, 0, 100, 100, 0, 0, u.hinst, 0)
	if h == 0 {
		log.Printf("CreateWindowEx: %v", err)
	}
	w := &ovWin{hwnd: h, paint: paint}
	u.wins[h] = w
	return w
}

func (u *ui) addTray() {
	u.nid = notifyIconData{HWnd: u.main, UID: 1, UFlags: nifMessage | nifIcon | nifTip, UCallbackMessage: wmTray}
	u.nid.CbSize = uint32(unsafe.Sizeof(u.nid))
	icon, _, _ := pLoadIconW.Call(u.hinst, 1) // exe に埋め込んだアイコン
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, 32512)
	}
	u.nid.HIcon = icon
	u.setTip("PhoneBoard（未接続）")
	pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&u.nid)))
}

func (u *ui) setTip(s string) {
	t, _ := syscall.UTF16FromString(s)
	copy(u.nid.SzTip[:len(u.nid.SzTip)-1], t)
}

func (u *ui) trayMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	pAppendMenuW.Call(menu, 0, 1, uintptr(unsafe.Pointer(u16("設定を開く"))))
	pAppendMenuW.Call(menu, 0, 2, uintptr(unsafe.Pointer(u16("キーボード表示の確認"))))
	pAppendMenuW.Call(menu, 0x800, 0, 0) // separator
	pAppendMenuW.Call(menu, 0, 3, uintptr(unsafe.Pointer(u16("終了"))))
	var pt [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(u.main)
	cmd, _, _ := pTrackPopupMenu.Call(menu, 0x0100|0x0002, uintptr(pt[0]), uintptr(pt[1]), 0, u.main, 0)
	pPostMessageW.Call(u.main, 0, 0, 0)
	pDestroyMenu.Call(menu)
	switch cmd {
	case 1:
		u.openSettings()
	case 2:
		u.app.Preview("")
	case 3:
		pPostQuitMessage.Call(0)
	}
}

func (u *ui) openSettings() {
	u.app.mu.Lock()
	url := u.app.webURL
	u.app.mu.Unlock()
	if url != "" {
		go openBrowser(url)
	}
}

func wndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	u := theUI
	switch msg {
	case wmPaint:
		if w, ok := u.wins[hwnd]; ok {
			u.paint(w)
			return 0
		}
	case wmEraseBkgnd:
		if _, ok := u.wins[hwnd]; ok {
			return 1
		}
	case wmNcHitTest:
		if _, ok := u.wins[hwnd]; ok {
			return ^uintptr(0) // HTTRANSPARENT: クリックを下のウィンドウへ通す
		}
	case wmMouseActivate:
		if _, ok := u.wins[hwnd]; ok {
			return 3 // MA_NOACTIVATE
		}
	case wmTimer:
		if hwnd == u.main {
			u.tick()
			return 0
		}
	case wmTray:
		switch lparam & 0xffff {
		case wmLButtonUp, wmLButtonDblClk:
			u.openSettings()
		case wmRButtonUp, wmContextMenu:
			u.trayMenu()
		}
		return 0
	}
	if u != nil && u.tbCreated != 0 && msg == u.tbCreated {
		pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&u.nid)))
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wparam, lparam)
	return r
}

func (u *ui) workArea() (rect, float64) {
	fg, _, _ := pGetForegroundWindow.Call()
	mon, _, _ := pMonitorFromWindow.Call(fg, 1) // MONITOR_DEFAULTTOPRIMARY
	mi := monitorInfo{}
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	pGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi)))
	dpi := uintptr(96)
	if pGetDpiForMonitor.Find() == nil {
		var dx, dy uint32
		if r, _, _ := pGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
			dpi = uintptr(dx)
		}
	} else if pGetDpiForSystem.Find() == nil {
		dpi, _, _ = pGetDpiForSystem.Call()
	}
	if mi.RcWork.R <= mi.RcWork.L {
		mi.RcWork = rect{0, 0, 1280, 720}
	}
	return mi.RcWork, float64(dpi) / 96
}

// placeCustom は画面に対する中心位置(0〜1)に置き、画面からはみ出さないようにする。
func placeCustom(px, py float64, wa rect, w, h int32) (int32, int32) {
	x := wa.L + int32(px*float64(wa.R-wa.L)) - w/2
	y := wa.T + int32(py*float64(wa.B-wa.T)) - h/2
	if x+w > wa.R {
		x = wa.R - w
	}
	if y+h > wa.B {
		y = wa.B - h
	}
	if x < wa.L {
		x = wa.L
	}
	if y < wa.T {
		y = wa.T
	}
	return x, y
}

func place(anchor string, wa rect, w, h, ox, oy int32) (int32, int32) {
	cx := wa.L + (wa.R-wa.L-w)/2
	x, y := cx, wa.B-h
	switch anchor {
	case "bottom-left":
		x = wa.L
	case "bottom-right":
		x = wa.R - w
	case "center":
		y = wa.T + (wa.B-wa.T-h)/2
	case "top":
		y = wa.T
	case "top-left":
		x, y = wa.L, wa.T
	case "top-right":
		x, y = wa.R-w, wa.T
	}
	return x + ox, y + oy
}

func (u *ui) tick() {
	d, ver := u.app.ov.Get()
	now := time.Now()
	changed := ver != u.ver
	u.data, u.ver = d, ver
	wa, scale := u.workArea()
	u.scale = scale
	c := d.Cfg
	preview := now.Before(d.PreviewUntil)
	anim := now.Sub(d.FlashAt) < 800*time.Millisecond

	// トレイのツールチップ
	if d.Connected != u.tipConn {
		u.tipConn = d.Connected
		if d.Connected {
			u.setTip("PhoneBoard（接続中）")
		} else {
			u.setTip("PhoneBoard（未接続）")
		}
		pShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&u.nid)))
	}

	// キーボード表示
	kbShow := d.KbVisible || preview
	if kbShow {
		w := int32(float64(c.Width) * scale)
		header := int32(40 * scale)
		pad := int32(8 * scale)
		aspect := d.Aspect
		if aspect <= 0.1 {
			aspect = 1.2
		}
		kh := int32(float64(w-2*pad) / aspect)
		maxH := (wa.B - wa.T) * 45 / 100
		if kh > maxH {
			kh = maxH
			w = int32(float64(kh)*aspect) + 2*pad
		}
		h := header + kh + 2*pad
		var x, y int32
		if c.Anchor == "custom" {
			x, y = placeCustom(c.PosX, c.PosY, wa, w, h)
		} else {
			x, y = place(c.Anchor, wa, w, h, int32(float64(c.OffsetX)*scale), int32(float64(c.OffsetY)*scale))
		}
		u.show(u.kb, x, y, w, h, c.Opacity, changed || anim)
	} else {
		u.hide(u.kb)
	}

	// マウスモードの小窓（触れている位置）
	miniShow := c.MiniMap && !kbShow && len(d.Zones) > 0 &&
		((d.Connected && (d.MiniTouch || now.Sub(d.MiniAt) < 400*time.Millisecond)) || preview)
	if miniShow {
		pa := d.PhoneAspect
		if pa <= 0.1 {
			pa = 0.47
		}
		w := int32(float64(c.MiniWidth) * scale)
		h := int32(float64(w)/pa) + int32(22*scale)
		x, y := place(c.MiniAnchor, wa, w, h, -int32(12*scale), -int32(12*scale))
		if strings.HasSuffix(c.MiniAnchor, "left") {
			x += int32(24 * scale)
		}
		if strings.HasPrefix(c.MiniAnchor, "top") {
			y += int32(24 * scale)
		}
		u.show(u.mini, x, y, w, h, c.Opacity, changed)
	} else {
		u.hide(u.mini)
	}

	// トースト（モード切替などのお知らせ）
	toastAge := now.Sub(d.ToastAt)
	if c.Toast && d.Toast != "" && toastAge < 1400*time.Millisecond {
		w := int32(360 * scale)
		h := int32(56 * scale)
		x := wa.L + (wa.R-wa.L-w)/2
		y := wa.T + (wa.B-wa.T)/4
		op := c.Opacity
		if toastAge > 1000*time.Millisecond {
			op = op * int(1400-toastAge.Milliseconds()) / 400
		}
		u.show(u.toast, x, y, w, h, op, true)
	} else {
		u.hide(u.toast)
	}
}

func (u *ui) show(w *ovWin, x, y, cw, ch int32, opacity int, redraw bool) {
	if opacity < 5 {
		opacity = 5
	}
	pSetLayeredWindowAttrs.Call(w.hwnd, colKeyT, uintptr(255*opacity/100), lwaAlpha|lwaColorKey)
	moved := x != w.x || y != w.y || cw != w.w || ch != w.h
	if !w.shown || moved || time.Since(w.lastTop) > time.Second {
		pSetWindowPos.Call(w.hwnd, hwndTopmost, uintptr(x), uintptr(y), uintptr(cw), uintptr(ch), swpNoActivate|swpShowWindow)
		w.x, w.y, w.w, w.h = x, y, cw, ch
		w.lastTop = time.Now()
		redraw = true
	}
	w.shown = true
	if redraw {
		pInvalidateRect.Call(w.hwnd, 0, 0)
	}
}

func (u *ui) hide(w *ovWin) {
	if w.shown {
		pShowWindow.Call(w.hwnd, swHide)
		w.shown = false
	}
}

// ===== 描画 =====

type canvas struct {
	u   *ui
	hdc uintptr
}

func rgb(r, g, b int) uintptr { return uintptr(r | g<<8 | b<<16) }

func (u *ui) font(px int, bold bool) uintptr {
	if px < 6 {
		px = 6
	}
	b := 0
	if bold {
		b = 1
	}
	k := [2]int{px, b}
	if f, ok := u.fonts[k]; ok {
		return f
	}
	weight := 400
	if bold {
		weight = 700
	}
	f, _, _ := pCreateFontW.Call(uintptr(uint32(int32(-px))), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(u16("Yu Gothic UI"))))
	u.fonts[k] = f
	return f
}

func (c *canvas) fill(x, y, w, h int32, col uintptr) {
	br, _, _ := pCreateSolidBrush.Call(col)
	r := rect{x, y, x + w, y + h}
	pFillRect.Call(c.hdc, uintptr(unsafe.Pointer(&r)), br)
	pDeleteObject.Call(br)
}

func (c *canvas) round(x, y, w, h, rad int32, fill uintptr, border uintptr, bw int32) {
	br, _, _ := pCreateSolidBrush.Call(fill)
	var pen uintptr
	if bw > 0 {
		pen, _, _ = pCreatePen.Call(0, uintptr(bw), border)
	} else {
		pen, _, _ = pGetStockObject.Call(8) // NULL_PEN
	}
	ob, _, _ := pSelectObject.Call(c.hdc, br)
	op, _, _ := pSelectObject.Call(c.hdc, pen)
	pRoundRect.Call(c.hdc, uintptr(x), uintptr(y), uintptr(x+w), uintptr(y+h), uintptr(rad), uintptr(rad))
	pSelectObject.Call(c.hdc, ob)
	pSelectObject.Call(c.hdc, op)
	pDeleteObject.Call(br)
	if bw > 0 {
		pDeleteObject.Call(pen)
	}
}

func (c *canvas) circle(cx, cy, r int32, fill, border uintptr, bw int32) {
	br, _, _ := pCreateSolidBrush.Call(fill)
	pen, _, _ := pCreatePen.Call(0, uintptr(bw), border)
	ob, _, _ := pSelectObject.Call(c.hdc, br)
	op, _, _ := pSelectObject.Call(c.hdc, pen)
	pEllipse.Call(c.hdc, uintptr(cx-r), uintptr(cy-r), uintptr(cx+r), uintptr(cy+r))
	pSelectObject.Call(c.hdc, ob)
	pSelectObject.Call(c.hdc, op)
	pDeleteObject.Call(br)
	pDeleteObject.Call(pen)
}

func (c *canvas) text(s string, x, y, w, h int32, px int, col uintptr, bold bool, align uintptr) {
	if s == "" {
		return
	}
	pSelectObject.Call(c.hdc, c.u.font(px, bold))
	pSetTextColor.Call(c.hdc, col)
	lines := strings.Split(s, "\n")
	lh := h / int32(len(lines))
	for i, ln := range lines {
		r := rect{x, y + lh*int32(i), x + w, y + lh*int32(i+1)}
		p, _ := syscall.UTF16FromString(ln)
		pDrawTextW.Call(c.hdc, uintptr(unsafe.Pointer(&p[0])), uintptr(len(p)-1), uintptr(unsafe.Pointer(&r)),
			align|dtVCenter|dtSingleLine|dtNoPrefix|dtEndEllipsis)
	}
}

func (u *ui) paint(w *ovWin) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
	var cr rect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&cr)))
	cw, ch := cr.R-cr.L, cr.B-cr.T
	mdc, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBitmap.Call(hdc, uintptr(cw), uintptr(ch))
	old, _, _ := pSelectObject.Call(mdc, bmp)
	pSetBkMode.Call(mdc, 1) // TRANSPARENT
	c := &canvas{u: u, hdc: mdc}
	c.fill(0, 0, cw, ch, colKeyT) // 透過色（角を丸く見せる）
	w.paint(c, cw, ch)
	pBitBlt.Call(hdc, 0, 0, uintptr(cw), uintptr(ch), mdc, 0, 0, srccopy)
	pSelectObject.Call(mdc, old)
	pDeleteObject.Call(bmp)
	pDeleteDC.Call(mdc)
	pEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
}

func slotLabel(s string) string {
	if IsAction(s) {
		return actionLabel(s)
	}
	return s
}

func (u *ui) px(v float64) int32 { return int32(v * u.scale) }

var (
	colKeyT    = rgb(255, 0, 255)
	colBg      = rgb(24, 24, 30)
	colKey     = rgb(48, 49, 60)
	colKeyFn   = rgb(36, 37, 46)
	colText    = rgb(235, 235, 240)
	colSub     = rgb(140, 142, 158)
	colHover   = rgb(120, 92, 20)
	colHoverBd = rgb(255, 200, 61)
	colArm     = rgb(24, 110, 60)
	colArmBd   = rgb(74, 222, 128)
	colDot     = rgb(255, 70, 90)
)

func (u *ui) paintKb(c *canvas, cw, ch int32) {
	d := &u.data
	kb := &d.Layout
	c.round(0, 0, cw, ch, u.px(14), colBg, rgb(70, 70, 90), 1)
	header := u.px(40)
	pad := u.px(8)

	// ヘッダー: モード / 入力履歴 / 状態
	title := d.Title
	if d.Caps {
		title += "(大)"
	}
	c.round(pad, u.px(7), u.px(58), header-u.px(14), u.px(10), rgb(60, 90, 200), 0, 0)
	c.text(title, pad, u.px(7), u.px(58), header-u.px(14), int(u.px(15)), colText, true, dtCenter)

	status := ""
	stCol := colSub
	sn := d.Snap
	// 右側: 今離すと入力されるもの（最重要）
	switch {
	case !d.Connected:
		status = "プレビュー"
	case sn.Touching && sn.Preview != "":
		status = "離すと " + slotLabel(sn.Preview)
		stCol = colHoverBd
		if sn.Armed {
			stCol = colArmBd
		}
	case sn.Touching && d.KbStyle == "dwell":
		status = "止めると確定待機"
		stCol = colHoverBd
	case sn.Touching:
		status = "離すと取消"
		stCol = rgb(255, 120, 130)
	case sn.Canceled:
		status = "取り消し"
	}
	flashAge := time.Since(d.FlashAt)
	right := status
	if flashAge < 700*time.Millisecond && d.Flash != "" && !sn.Touching {
		right = "入力 " + d.Flash
		stCol = colArmBd
	}
	rw := (cw - 2*pad) * 55 / 100
	c.text(right, cw-rw-pad, 0, rw, header, int(u.px(16)), stCol, true, dtRight)
	if d.Cfg.ShowHistory {
		hist := []rune(d.History)
		if len(hist) > 12 {
			hist = hist[len(hist)-12:]
		}
		hx := pad + u.px(64)
		c.text(string(hist), hx, 0, cw-rw-pad-hx-u.px(4), header, int(u.px(14)), colSub, false, dtLeft)
	}

	// キーボード
	ax, ay := pad, header
	aw, ah := cw-2*pad, ch-header-pad
	if kb.Cols <= 0 || kb.Rows <= 0 {
		return
	}
	cellW := float64(aw) / float64(kb.Cols)
	cellH := float64(ah) / float64(kb.Rows)
	gap := u.px(3)
	keyRect := func(k *Key) (int32, int32, int32, int32) {
		x := ax + int32(k.X*cellW) + gap
		y := ay + int32(k.Y*cellH) + gap
		return x, y, int32(k.W*cellW) - 2*gap, int32(k.H*cellH) - 2*gap
	}
	base := math.Min(cellW, cellH)
	for i := range kb.Keys {
		k := &kb.Keys[i]
		x, y, w, h := keyRect(k)
		fill, bd, bw := colKey, rgb(0, 0, 0), int32(0)
		if IsAction(k.C) {
			fill = colKeyFn
		}
		if sn.Touching && !sn.Armed && sn.Hover == i {
			fill, bd, bw = colHover, colHoverBd, u.px(3)
		}
		if sn.Armed && sn.ArmKey == i {
			fill, bd, bw = colArm, colArmBd, u.px(3)
		}
		c.round(x, y, w, h, u.px(8), fill, bd, bw)
		lf := base * 0.30
		if n := strings.Count(k.Label, "\n") + 1; n > 1 {
			lf = base * 0.30 / math.Sqrt(float64(n))
		}
		c.text(k.Label, x, y, w, h, int(lf), colText, true, dtCenter)
		if d.Cfg.ShowHints {
			hp := int(base * 0.15)
			hs := int32(float64(hp) * 1.4)
			c.text(slotLabel(k.L), x+u.px(3), y, w/3, h, hp, colSub, false, dtLeft)
			c.text(slotLabel(k.R), x+w-w/3-u.px(3), y, w/3, h, hp, colSub, false, dtRight)
			c.text(slotLabel(k.U), x, y+u.px(1), w, hs, hp, colSub, false, dtCenter)
			c.text(slotLabel(k.D), x, y+h-hs-u.px(1), w, hs, hp, colSub, false, dtCenter)
		}
	}

	// 確定待機中は十字のフリック候補を大きく表示
	if sn.Armed && sn.ArmKey >= 0 && sn.ArmKey < len(kb.Keys) {
		k := &kb.Keys[sn.ArmKey]
		x, y, w, h := keyRect(k)
		cx, cy := x+w/2, y+h/2
		s := int32(base*0.78) - gap
		pos := map[Dir][2]int32{DirC: {0, 0}, DirL: {-1, 0}, DirU: {0, -1}, DirR: {1, 0}, DirD: {0, 1}}
		for _, dd := range []Dir{DirL, DirU, DirR, DirD, DirC} {
			txt := slotLabel(k.Slot(dd))
			if txt == "" && dd != DirC {
				continue
			}
			p := pos[dd]
			bx := cx - s/2 + p[0]*(s+gap)
			by := cy - s/2 + p[1]*(s+gap)
			f, b := rgb(40, 60, 48), rgb(74, 222, 128)
			bw := u.px(1)
			if dd == sn.Dir {
				f, b, bw = rgb(34, 160, 80), rgb(200, 255, 210), u.px(3)
			}
			c.round(bx, by, s, s, u.px(10), f, b, bw)
			fs := base * 0.36
			if len([]rune(txt)) > 2 {
				fs = base * 0.22
			}
			c.text(txt, bx, by, s, s, int(fs), colText, true, dtCenter)
		}
	}

	// 指の位置
	if sn.Touching {
		fx := float64(ax) + sn.RX*float64(aw)
		fy := float64(ay) + sn.RY*float64(ah)
		fx = math.Max(float64(ax), math.Min(float64(ax+aw), fx))
		fy = math.Max(float64(ay), math.Min(float64(ay+ah), fy))
		r := u.px(8)
		if sn.Inside {
			c.circle(int32(fx), int32(fy), r, colDot, rgb(255, 255, 255), u.px(2))
		} else {
			c.circle(int32(fx), int32(fy), r, colBg, colDot, u.px(3))
		}
	}
}

func (u *ui) paintMini(c *canvas, cw, ch int32) {
	d := &u.data
	c.round(0, 0, cw, ch, u.px(10), colBg, rgb(70, 70, 90), 1)
	head := u.px(22)
	label := "マウス"
	if d.TapRight {
		label += " / タップ=右"
	}
	if d.ScrollMode {
		label += " / スクロール"
	}
	c.text(label, 0, 0, cw, head, int(u.px(12)), colText, true, dtCenter)
	pad := u.px(5)
	px, py, pw, ph := pad, head, cw-2*pad, ch-head-pad
	c.round(px, py, pw, ph, u.px(6), rgb(10, 10, 12), rgb(90, 90, 110), 1)
	for i, z := range d.Zones {
		x := px + int32(z.X*float64(pw))
		y := py + int32(z.Y*float64(ph))
		w := int32(z.W * float64(pw))
		h := int32(z.H * float64(ph))
		fill := rgb(36, 37, 46)
		switch z.Type {
		case "scroll":
			fill = rgb(30, 50, 70)
		case "button":
			fill = rgb(55, 45, 70)
		}
		bd, bw := rgb(70, 70, 90), int32(1)
		if d.MiniTouch && d.MiniZone == i {
			fill, bd, bw = colHover, colHoverBd, u.px(2)
		}
		c.round(x+1, y+1, w-2, h-2, u.px(4), fill, bd, bw)
		c.text(z.Label, x, y, w, h, int(u.px(9)), colSub, false, dtCenter)
	}
	if d.MiniTouch {
		fx := px + int32(d.MiniX*float64(pw))
		fy := py + int32(d.MiniY*float64(ph))
		c.circle(fx, fy, u.px(5), colDot, rgb(255, 255, 255), u.px(1))
	}
}

func (u *ui) paintToast(c *canvas, cw, ch int32) {
	c.round(0, 0, cw, ch, u.px(16), rgb(30, 30, 40), rgb(110, 110, 140), 1)
	c.text(u.data.Toast, 0, 0, cw, ch, int(u.px(22)), colText, true, dtCenter)
}
