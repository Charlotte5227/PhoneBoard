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
		u.app.Preview()
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
	anim := false
	for _, ft := range d.Flash {
		if now.Sub(ft) < 400*time.Millisecond {
			anim = true
		}
	}

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

	// キーボード表示（スマホ画面全体＝キーボードなので、スマホの縦横比で描く）
	kbShow := d.KbVisible || preview
	if kbShow {
		w := int32(float64(c.Width) * scale)
		header := u.px(30)
		pad := u.px(6)
		aspect := d.Aspect
		if aspect <= 0.3 {
			aspect = 2.2
		}
		kh := int32(float64(w-2*pad) / aspect)
		maxH := (wa.B - wa.T) * 50 / 100
		if kh > maxH {
			kh = maxH
			w = int32(float64(kh)*aspect) + 2*pad
		}
		h := header + kh + pad
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
		pa := d.Aspect
		if pa <= 0.1 {
			pa = 2.2
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
	colDot     = rgb(255, 70, 90)
)

var (
	colOnce  = rgb(150, 110, 20) // ワンショット
	colLock  = rgb(30, 130, 70)  // 固定
	colHeld  = rgb(40, 90, 190)  // 押さえている
	colFlash = rgb(60, 200, 110) // 入力した
	colFn    = rgb(120, 220, 255)
)

func (u *ui) paintKb(c *canvas, cw, ch int32) {
	d := &u.data
	b := &d.Board
	kv := &d.Kbd
	c.round(0, 0, cw, ch, u.px(12), colBg, rgb(70, 70, 90), 1)
	header := u.px(30)
	pad := u.px(6)

	// ヘッダー: IMEの状態 / 入力履歴
	imeTxt, imeCol := "A", rgb(90, 92, 110)
	if d.IME {
		imeTxt, imeCol = "あ", rgb(60, 90, 200)
	}
	c.round(pad, u.px(4), u.px(34), header-u.px(8), u.px(8), imeCol, 0, 0)
	c.text(imeTxt, pad, u.px(4), u.px(34), header-u.px(8), int(u.px(15)), colText, true, dtCenter)
	info := ""
	if !d.Connected {
		info = "プレビュー（未接続）"
	} else if d.Cfg.ShowHistory {
		h := []rune(d.History)
		if len(h) > 40 {
			h = h[len(h)-40:]
		}
		info = string(h)
	}
	c.text(info, pad+u.px(44), 0, cw-2*pad-u.px(44), header, int(u.px(14)), colSub, false, dtRight)

	ax, ay := pad, header
	aw, ah := cw-2*pad, ch-header-pad
	if b.Cols <= 0 || b.Rows <= 0 {
		return
	}
	ux := float64(aw) / b.Cols
	uy := float64(ah) / b.Rows
	gap := u.px(2)
	base := math.Min(ux, uy)
	hover := map[int]bool{}
	for _, f := range kv.Fingers {
		if f.Key >= 0 {
			hover[f.Key] = true
		}
	}
	now := time.Now()
	for i := range b.Keys {
		k := &b.Keys[i]
		x := ax + int32(k.X*ux) + gap
		y := ay + int32(k.Y*uy) + gap
		w := int32(k.W*ux) - 2*gap
		h := int32(k.H*uy) - 2*gap
		fill, bd, bw := colKey, rgb(0, 0, 0), int32(0)
		if k.Letter() == 0 && len([]rune(k.Label)) > 1 {
			fill = colKeyFn
		}
		if k.Mod != "" {
			switch kv.Mods[k.Mod] {
			case "once":
				fill = colOnce
			case "lock":
				fill = colLock
			case "held":
				fill = colHeld
			}
		}
		if ft, ok := d.Flash[i]; ok && now.Sub(ft) < 250*time.Millisecond {
			fill = colFlash
		}
		if hover[i] {
			bd, bw = colHoverBd, u.px(3)
			if fill == colKey || fill == colKeyFn {
				fill = colHover
			}
		}
		c.round(x, y, w, h, u.px(6), fill, bd, bw)

		main, sub, mainCol := k.Label, k.Shift, colText
		if kv.Fn && k.FnLabel != "" {
			main, sub, mainCol = k.FnLabel, "", colFn
		} else if kv.Shift && k.Shift != "" {
			main, sub = k.Shift, k.Label
		}
		fs := base * 0.42
		if n := len([]rune(main)); n > 1 {
			fs = base * 0.27
			if n > 3 {
				fs = base * 0.22
			}
		}
		c.text(main, x, y, w, h, int(fs), mainCol, true, dtCenter)
		if sub != "" && d.Cfg.ShowHints {
			c.text(sub, x+u.px(3), y+u.px(1), w/2, int32(base*0.3), int(base*0.2), colSub, false, dtLeft)
		}
	}
	// 指の位置（両手分）
	for _, f := range kv.Fingers {
		fx := ax + int32(f.X*float64(aw))
		fy := ay + int32(f.Y*float64(ah))
		c.circle(fx, fy, u.px(7), colDot, rgb(255, 255, 255), u.px(2))
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
