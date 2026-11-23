package main

import (
	"log"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Sender はスマホへの送信口（server.go の Session）。
type Sender interface {
	Send(v any)
	Close()
	Name() string
}

type PhoneInfo struct {
	Name string  `json:"name"`
	Wdp  float64 `json:"wdp"`
	Hdp  float64 `json:"hdp"`
}

// Msg はスマホから届くメッセージ（全種類の項目をまとめた形）。
type Msg struct {
	T    string      `json:"t"`
	A    string      `json:"a"`
	B    string      `json:"b"`
	K    string      `json:"k"`
	S    string      `json:"s"`
	M    string      `json:"m"`
	X    float64     `json:"x"`
	Y    float64     `json:"y"`
	Dx   float64     `json:"dx"`
	Dy   float64     `json:"dy"`
	N    int         `json:"n"`
	Z    int         `json:"z"`
	ID   int         `json:"id"`
	Wdp  float64     `json:"wdp"`
	Hdp  float64     `json:"hdp"`
	Name string      `json:"name"`
	Pin  string      `json:"pin"`
	V    int         `json:"v"`
	Ts   int64       `json:"ts"`
	Tok  string      `json:"tok"` // UDPで届くメッセージの合言葉
	Pts  [][]float64 `json:"pts"` // 英字の一筆書きの軌跡
	U    bool        `json:"u"`   // 大文字（もう片方の指で押さえている）
}

type App struct {
	mu         sync.Mutex
	cfg        *Config
	lay        *Layouts
	inj        Injector
	kbd        *Kbd
	ov         *OverlayModel
	sess       Sender
	phone      PhoneInfo
	mode       string // mouse / keyboard
	tapRight   bool
	scrollMode bool
	held       map[int]bool
	dir        *Dir           // 方向入力
	heldKeys   map[uint16]int // 押しっぱなしのキー
	fx, fy     float64
	wy, wx     float64
	webURL     string
	start      time.Time
	history    []rune
	imeOn      bool
	imeAt      time.Time
	clicker    Clicker
	OpenURL    func(string)
	tsOff      int64  // スマホの時刻 → PCの時刻
	udpTok     string // 今の接続のUDP用合言葉
	lastFg     uintptr
	calibDirty int
}

func NewApp(cfg *Config, lay *Layouts, inj Injector) *App {
	a := &App{
		cfg:   cfg,
		lay:   lay,
		inj:   inj,
		ov:    &OverlayModel{},
		mode:  "mouse",
		held:  map[int]bool{},
		phone: PhoneInfo{Wdp: 915, Hdp: 412},
		start: time.Now(),
	}
	a.kbd = NewKbd(a)
	a.dir = NewDir(dirAdapter{a}, LoadRecognizer())
	a.kbd.IME = func() bool { return a.imeOn }
	_ = loadJSON(filepath.Join(dataDir(), "calib.json"), &a.kbd.Calib)
	a.kbd.Learned = func() {
		a.calibDirty++
		if a.calibDirty >= 20 {
			a.saveCalibLocked()
		}
	}
	a.clicker = NewClicker()
	a.applyLocked()
	return a
}

func (a *App) now() int64 { return time.Since(a.start).Milliseconds() }

func (a *App) saveCalibLocked() {
	a.calibDirty = 0
	c := a.kbd.Calib
	go func() { _ = saveJSON(filepath.Join(dataDir(), "calib.json"), &c) }()
}

// ResetCalib はタッチ位置の自動補正を初期化する（設定画面から）。
func (a *App) ResetCalib() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.kbd.ResetCalib()
	a.saveCalibLocked()
}

// applyLocked は設定・配列の変更をキーボードとオーバーレイへ反映する。
func (a *App) applyLocked() {
	a.inj.SetScanCodes(a.cfg.Keyboard.ScanCodes)
	a.kbd.Configure(a.cfg.Keyboard, &a.lay.Board)
	a.refreshOverlayLocked()
}

func (a *App) refreshOverlayLocked() {
	view := a.kbd.View()
	flash := a.kbd.TakeFlash()
	hist := a.history
	if len(hist) > 40 {
		hist = hist[len(hist)-40:]
	}
	a.ov.Update(func(m *OverlayData) {
		m.Cfg = a.cfg.Overlay
		m.KbVisible = a.mode == "keyboard" && a.sess != nil
		m.Board = a.lay.Board
		m.Aspect = a.phone.Wdp / math.Max(a.phone.Hdp, 1)
		if m.Aspect < 1 { // 横向き専用なので、縦長の寸法が来ても横長として扱う
			m.Aspect = 1 / math.Max(m.Aspect, 0.01)
		}
		m.Kbd = view
		now := time.Now()
		for _, i := range flash {
			if m.Flash == nil {
				m.Flash = map[int]time.Time{}
			}
			m.Flash[i] = now
		}
		m.History = string(hist)
		m.IME = a.imeOn
		m.Zones = a.lay.MouseZones
		if a.mode == "game" {
			m.Zones = a.lay.GameZones
		}
		m.TapRight = a.tapRight
		m.ScrollMode = a.scrollMode
		m.Connected = a.sess != nil
	})
}

func (a *App) toast(s string) {
	a.ov.Update(func(m *OverlayData) {
		m.Toast = s
		m.ToastAt = time.Now()
	})
}

func (a *App) send(v any) {
	if a.sess != nil {
		a.sess.Send(v)
	}
}

func (a *App) vib(kind string) {
	h := a.cfg.Haptic
	if !h.Enabled || a.sess == nil {
		return
	}
	ms := 0
	switch kind {
	case "hover":
		ms = h.HoverMs
	case "commit":
		ms = h.CommitMs
	case "toggle", "mod":
		ms = h.ToggleMs
	case "cancel":
		if h.CommitMs > 0 {
			a.send(map[string]any{"t": "vib", "pat": []int{0, 12, 60, 12}, "amp": h.Amp})
		}
		return
	}
	if ms > 0 {
		a.send(map[string]any{"t": "vib", "ms": ms, "amp": h.Amp})
	}
}

// sound は音を鳴らす（設定に応じてPC/スマホ）。semi は音の高さ（半音、0〜12）。
func (a *App) sound(group string, semi int) {
	k := a.cfg.Keyboard
	vol := k.ClickVolume
	if group == SndWarn {
		vol = k.WarnVolume
	} else if !k.KeyTones {
		group, semi = SndLetter, 6
	}
	if vol <= 0 || k.ClickSound == "off" {
		return
	}
	if (k.ClickSound == "pc" || k.ClickSound == "both") && a.clicker != nil {
		a.clicker.Play(group, semi, vol)
	}
	if k.ClickSound == "phone" || k.ClickSound == "both" {
		a.send(map[string]any{"t": "click", "g": group, "p": semi, "vol": vol})
	}
}

var symbolKeys = map[string]bool{"MINUS": true, "CARET": true, "YEN": true, "AT": true, "LBRACKET": true, "RBRACKET": true,
	"SEMICOLON": true, "COLON": true, "COMMA": true, "PERIOD": true, "SLASH": true, "BACKSLASH": true}

// keyTone はキーの音色（系統）と高さ（横位置: 左が低く右が高い）。
func (a *App) keyTone(ev KeyEvent) (string, int) {
	b := &a.lay.Board
	semi := 6
	if ev.Index >= 0 && ev.Index < len(b.Keys) && b.Cols > 0 {
		k := &b.Keys[ev.Index]
		semi = int(math.Round((k.X + k.W/2) / b.Cols * 12))
	}
	switch {
	case ev.Letter != 0 && isVowel(ev.Letter):
		return SndVowel, semi
	case ev.Letter != 0:
		return SndLetter, semi
	case ev.Name == "SPACE" || ev.Name == "ENTER" || ev.Name == "BS":
		return SndHeavy, semi
	case !ev.Fn && (symbolKeys[ev.Name] || (len(ev.Name) == 1 && ev.Name[0] >= '0' && ev.Name[0] <= '9')):
		return SndSymbol, semi
	}
	return SndFunc, semi
}

// echo は入力したキーを画面中央にうっすら表示する。
func (a *App) echo(s string, warn bool) {
	if !a.cfg.Overlay.Echo {
		return
	}
	a.ov.Update(func(m *OverlayData) {
		m.Echo = s
		m.EchoWarn = warn
		m.EchoAt = time.Now()
	})
}

// ===== キーボードエンジンからの出力（App のロック中に呼ばれる） =====

func (a *App) KeyPress(ev KeyEvent) {
	a.inj.KeyCombo(ev.Mods, ev.VK)
	if ev.Warn != "" {
		a.sound(SndWarn, 0)
	} else {
		a.sound(a.keyTone(ev))
	}
	a.vib("commit")
	vk := ev.VK
	label := ev.Label
	a.echo(label, ev.Warn != "")
	// 表示用の入力履歴（あくまで目安）
	switch {
	case vk == vkBack:
		if len(a.history) > 0 {
			a.history = a.history[:len(a.history)-1]
		}
	case vk == vkReturn:
		a.history = append(a.history, '⏎')
	case vk == vkSpace:
		a.history = append(a.history, '␣')
	case len(ev.Mods) == 0 || (len(ev.Mods) == 1 && ev.Mods[0] == modVK["shift"]):
		if r := []rune(label); len(r) == 1 {
			a.history = append(a.history, r[0])
		}
	}
	if len(a.history) > 200 {
		a.history = append([]rune{}, a.history[100:]...)
	}
	if vk == 0x19 { // 半/全 → IMEの状態がすぐ変わる
		a.imeAt = time.Time{}
	}
}

func (a *App) ModKey(vk uint16, down bool) {
	if down {
		a.inj.KeyDown(vk)
	} else {
		a.inj.KeyUp(vk)
	}
}

func (a *App) Action(s string) {
	a.sound(SndFunc, 0)
	a.doActionLocked(s, "tap")
}

func (a *App) Feedback(kind string) {
	switch kind {
	case "hover":
		a.vib("hover")
	case "mod":
		a.vib("mod")
		a.sound(SndMod, 6)
	case "cancel":
		a.vib("cancel")
	}
}

// ===== アクション =====

func isClickAction(name string) (int, bool) {
	switch name {
	case "CLICK_L":
		return 0, true
	case "CLICK_R":
		return 1, true
	case "CLICK_M":
		return 2, true
	}
	return 0, false
}

// doActionLocked はアクションを実行する。phase: tap / down / up
func (a *App) doActionLocked(s string, phase string) {
	if !IsAction(s) {
		return
	}
	name := actionName(s)
	if btn, ok := isClickAction(name); ok {
		a.kbd.ResetContext()
		switch phase {
		case "down":
			a.inj.MouseButton(btn, true)
			a.held[btn] = true
		case "up":
			a.inj.MouseButton(btn, false)
			delete(a.held, btn)
		default:
			a.inj.MouseButton(btn, true)
			a.inj.MouseButton(btn, false)
		}
		return
	}
	if phase == "up" {
		return
	}
	switch name {
	case "MOUSE":
		a.setModeLocked("mouse")
	case "KEYBOARD":
		a.setModeLocked("keyboard")
	case "TOGGLE": // マウス → キーボード →（使うなら）方向入力 →（使うなら）ゲーム → マウス
		switch {
		case a.mode == "mouse":
			a.setModeLocked("keyboard")
		case a.mode == "keyboard" && a.cfg.Dir.Enabled:
			a.setModeLocked("dir")
		case (a.mode == "keyboard" || a.mode == "dir") && a.cfg.Game.Enabled:
			a.setModeLocked("game")
		default:
			a.setModeLocked("mouse")
		}
	case "GAME":
		a.setModeLocked("game")
	case "DIRECT", "DIR":
		a.setModeLocked("dir")
	case "TAPMODE":
		a.tapRight = !a.tapRight
		a.vib("toggle")
		if a.tapRight {
			a.toast("タップ = 右クリック")
		} else {
			a.toast("タップ = 左クリック")
		}
		a.sendStateLocked()
	case "SCROLLMODE":
		a.scrollMode = !a.scrollMode
		a.vib("toggle")
		if a.scrollMode {
			a.toast("パッド = スクロール")
		} else {
			a.toast("パッド = カーソル移動")
		}
		a.sendStateLocked()
	case "DBLCLICK":
		for i := 0; i < 2; i++ {
			a.inj.MouseButton(0, true)
			a.inj.MouseButton(0, false)
		}
	case "SETTINGS":
		if a.OpenURL != nil && a.webURL != "" {
			go a.OpenURL(a.webURL)
		}
	case "CLEAR":
		a.history = a.history[:0]
	case "NONE", "":
	default:
		if mods, vk, ok := ParseCombo(name); ok {
			a.inj.KeyCombo(mods, vk)
			if vk == 0x19 {
				a.imeAt = time.Time{}
			}
		}
	}
	a.refreshOverlayLocked()
}

func (a *App) setModeLocked(m string) {
	if m != "mouse" && m != "keyboard" && m != "game" && m != "dir" {
		return
	}
	a.mode = m
	a.kbd.CancelAll()
	a.kbd.ResetContext()
	a.releaseHeldLocked()
	a.toast(map[string]string{"mouse": "マウス", "keyboard": "キーボード", "game": "ゲーム", "dir": "方向入力"}[m])
	a.vib("toggle")
	a.send(map[string]any{"t": "setmode", "m": m})
	if m == "dir" {
		a.dir.Enter()
	}
	a.refreshOverlayLocked()
}

func (a *App) releaseHeldLocked() {
	for b := range a.held {
		a.inj.MouseButton(b, false)
	}
	a.held = map[int]bool{}
	for vk, n := range a.heldKeys {
		if n > 0 {
			a.inj.KeyUp(vk)
		}
	}
	a.heldKeys = map[uint16]int{}
}

// keyHoldLocked は押しっぱなしのキーを押す/離す（同じキーを2本の指で押しても正しく数える）。
func (a *App) keyHoldLocked(vk uint16, down bool) {
	if a.heldKeys == nil {
		a.heldKeys = map[uint16]int{}
	}
	if down {
		a.heldKeys[vk]++
		if a.heldKeys[vk] == 1 {
			a.inj.KeyDown(vk)
		}
		return
	}
	if a.heldKeys[vk] > 0 {
		a.heldKeys[vk]--
		if a.heldKeys[vk] == 0 {
			a.inj.KeyUp(vk)
		}
	}
}

// KeyHold は押しっぱなし方式（ゲーム向け）のキーボードのキー。
func (a *App) KeyHold(ev KeyEvent, down bool) {
	a.keyHoldLocked(ev.VK, down)
	if down {
		a.sound(a.keyTone(ev))
		a.vib("commit")
	}
}

// appActions はPhoneBoard自身の動作（キー送信ではない）アクション。
var appActions = map[string]bool{"MOUSE": true, "KEYBOARD": true, "TOGGLE": true, "GAME": true, "TAPMODE": true,
	"SCROLLMODE": true, "DBLCLICK": true, "SETTINGS": true, "CLEAR": true, "NONE": true}

// holdButtonLocked はボタン領域のキーを押している間押しっぱなしにする（修飾キー付きなら修飾キーも）。
func (a *App) holdButtonLocked(act string, down bool) bool {
	if !IsAction(act) {
		return false
	}
	name := actionName(act)
	if appActions[name] || strings.HasPrefix(name, "MODE") {
		return false
	}
	mods, vk, ok := ParseCombo(name)
	if !ok {
		return false
	}
	if down {
		for _, m := range mods {
			a.keyHoldLocked(m, true)
		}
		a.keyHoldLocked(vk, true)
	} else {
		a.keyHoldLocked(vk, false)
		for i := len(mods) - 1; i >= 0; i-- {
			a.keyHoldLocked(mods[i], false)
		}
	}
	return true
}

func (a *App) sendStateLocked() {
	a.send(map[string]any{"t": "state", "tapRight": a.tapRight, "scrollMode": a.scrollMode})
}

type keyView struct {
	Label string  `json:"label"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
}

func (a *App) sendCfgLocked() {
	if a.sess == nil {
		return
	}
	b := &a.lay.Board
	keys := make([]keyView, 0, len(b.Keys))
	for _, k := range b.Keys {
		keys = append(keys, keyView{k.Label, k.X, k.Y, k.W, k.H})
	}
	m := a.cfg.Mouse
	a.send(map[string]any{
		"t": "cfg",
		"mouse": map[string]any{
			"zones": a.lay.MouseZones, "tapDrag": m.TapDrag, "tapDelayMs": m.TapDelayMs,
			"longPress": m.LongPress, "longPressMs": m.LongPressMs, "momentum": m.Momentum,
			"miniMap": a.cfg.Overlay.MiniMap,
		},
		"kb":   map[string]any{"cols": b.Cols, "rows": b.Rows, "keys": keys},
		"game": map[string]any{"zones": a.lay.GameZones},
		"dir": map[string]any{"flickDp": a.cfg.Dir.FlickDp, "bendDp": a.cfg.Dir.BendDp,
			"repeatDelayMs": a.cfg.Keyboard.RepeatDelayMs, "repeatIntervalMs": a.cfg.Keyboard.RepeatIntervalMs},
		"haptic": a.cfg.Haptic.Enabled,
	})
	a.sendStateLocked()
	if a.mode == "dir" {
		a.send(map[string]any{"t": "dsub", "m": a.dir.Sub})
	}
}

// ===== 接続 =====

func (a *App) OnConnect(s Sender, info PhoneInfo, udpTok string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.sess
	a.sess = s
	a.udpTok = udpTok
	a.tsOff = 0
	if old != nil && old != s {
		old.Close()
	}
	if info.Wdp > 0 && info.Hdp > 0 {
		a.phone = info
	} else {
		a.phone.Name = info.Name
	}
	a.mode = "mouse"
	a.applyLocked()
	a.sendCfgLocked()
	a.send(map[string]any{"t": "setmode", "m": a.mode})
	a.toast("スマホ接続: " + info.Name)
	log.Printf("connected: %s (%.0fx%.0fdp)", info.Name, info.Wdp, info.Hdp)
}

func (a *App) OnDisconnect(s Sender) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess != s {
		return
	}
	a.sess = nil
	a.udpTok = ""
	a.kbd.CancelAll()
	a.releaseHeldLocked()
	if a.calibDirty > 0 {
		a.saveCalibLocked()
	}
	a.refreshOverlayLocked()
	a.toast("スマホ切断")
	log.Printf("disconnected")
}

// Handle はスマホからのメッセージを処理する。
func (a *App) Handle(s Sender, m *Msg) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess != s {
		return
	}
	t := a.now()
	switch m.T {
	case "kt": // キーボードのタッチ（指ごと）
		if a.mode != "keyboard" {
			return
		}
		// スマホ側の時刻を使う（Wi-Fiの遅れの揺れで「離す直前」の判定が狂わないように）
		if m.Ts > 0 {
			if a.tsOff == 0 {
				a.tsOff = t - m.Ts
			}
			if mt := m.Ts + a.tsOff; mt <= t {
				t = mt
			} else {
				a.tsOff = t - m.Ts
			}
		}
		switch m.A {
		case "d":
			a.kbd.Down(m.ID, m.X, m.Y, t)
		case "m":
			a.kbd.Move(m.ID, m.X, m.Y, t)
		case "u":
			a.kbd.Up(m.ID, m.X, m.Y, t)
		default:
			if f, ok := a.kbd.fingers[m.ID]; ok {
				a.kbd.release(f, false)
			}
		}
		a.refreshOverlayLocked()
	case "df": // 方向入力: はじき1回
		if a.mode == "dir" {
			a.dir.Flick(m.S, m.N, m.Z != 0, m.B)
			a.vib("commit")
		}
	case "dp": // 方向入力: 切り替え口に入った・曲げた（音で知らせる）
		if a.mode == "dir" {
			a.dir.Preview(m.S, m.N, m.B)
		}
	case "ds": // 英字の一筆書き
		if a.mode == "dir" {
			raw := make([]pt, 0, len(m.Pts))
			for _, q := range m.Pts {
				if len(q) >= 2 {
					raw = append(raw, pt{q[0], q[1]})
				}
			}
			a.dir.Stroke(raw, m.U)
			a.vib("commit")
		}
	case "d2": // 両手の親指で同時にトン
		if a.mode == "dir" {
			a.dir.TwoTap()
		}
	case "mm":
		a.mouseMoveLocked(m.Dx, m.Dy)
	case "lm":
		a.lookMoveLocked(m.Dx, m.Dy)
	case "ms":
		a.scrollLocked(m.Dx, m.Dy)
	case "tap":
		a.kbd.ResetContext()
		btn := 0
		switch {
		case m.B == "r":
			btn = 1
		case m.B == "m":
			btn = 2
		case m.B == "" && a.tapRight:
			btn = 1
			if a.cfg.Mouse.RightClickOneShot {
				a.tapRight = false
				a.sendStateLocked()
				a.refreshOverlayLocked()
			}
		}
		n := m.N
		if n < 1 {
			n = 1
		}
		if a.mode == "game" && n == 1 {
			// ゲームは1フレームより短いクリックを取りこぼすことがあるので、少し押したままにする
			if a.held[btn] {
				break
			}
			a.inj.MouseButton(btn, true)
			time.AfterFunc(40*time.Millisecond, func() {
				a.mu.Lock()
				defer a.mu.Unlock()
				if !a.held[btn] {
					a.inj.MouseButton(btn, false)
				}
			})
			break
		}
		for i := 0; i < n && i < 3; i++ {
			a.inj.MouseButton(btn, true)
			a.inj.MouseButton(btn, false)
		}
	case "mb":
		btn := map[string]int{"l": 0, "r": 1, "m": 2}[m.B]
		switch m.A {
		case "down":
			a.inj.MouseButton(btn, true)
			a.held[btn] = true
		case "up":
			a.inj.MouseButton(btn, false)
			delete(a.held, btn)
		}
	case "btn": // マウス画面のボタン領域
		if m.S == "down" || m.S == "up" {
			name := ""
			if IsAction(m.A) {
				name = actionName(m.A)
			}
			if _, ok := isClickAction(name); ok {
				a.doActionLocked(m.A, m.S)
			} else if a.holdButtonLocked(m.A, m.S == "down") {
				// キーは押している間押しっぱなし（ゲームの移動キーなど）
				if m.S == "down" {
					a.sound(SndFunc, 6)
				}
			} else if m.S == "up" {
				a.doActionLocked(m.A, "tap")
			}
		}
	case "act":
		a.doActionLocked(m.A, "tap")
	case "hw": // 音量キー
		up := m.K == "volup"
		var act string
		if a.mode == "keyboard" || a.mode == "dir" {
			act = map[bool]string{true: a.cfg.Buttons.KbVolUp, false: a.cfg.Buttons.KbVolDown}[up]
		} else {
			act = map[bool]string{true: a.cfg.Buttons.MouseVolUp, false: a.cfg.Buttons.MouseVolDown}[up]
		}
		if act == "" {
			return
		}
		name := ""
		if IsAction(act) {
			name = actionName(act)
		}
		if _, ok := isClickAction(name); ok {
			a.doActionLocked(act, m.A)
		} else if m.A == "down" {
			a.doActionLocked(act, "tap")
		}
	case "mt": // マウスモードで触れている位置（PC画面の小窓表示用）
		a.ov.Update(func(o *OverlayData) {
			o.MiniTouch = m.A != "u" && m.A != "c"
			o.MiniX, o.MiniY = m.X, m.Y
			o.MiniZone = m.Z
			o.MiniAt = time.Now()
		})
	case "size":
		if m.Wdp > 0 && m.Hdp > 0 {
			a.phone.Wdp, a.phone.Hdp = m.Wdp, m.Hdp
			a.refreshOverlayLocked()
		}
	case "ping":
		a.send(map[string]any{"t": "pong"})
	}
}

// HandleUDP はUDPで届いたマウスの動き（カーソル移動・スクロール・小窓）を処理する。
// 取りこぼしても待たないので、Wi-Fiが一瞬途切れてもカーソルが止まりにくい。
func (a *App) HandleUDP(m *Msg) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil || a.udpTok == "" || m.Tok != a.udpTok {
		return
	}
	switch m.T {
	case "mm":
		a.mouseMoveLocked(m.Dx, m.Dy)
	case "lm":
		a.lookMoveLocked(m.Dx, m.Dy)
	case "ms":
		a.scrollLocked(m.Dx, m.Dy)
	case "mt":
		a.ov.Update(func(o *OverlayData) {
			o.MiniTouch = m.A != "u" && m.A != "c"
			o.MiniX, o.MiniY = m.X, m.Y
			o.MiniZone = m.Z
			o.MiniAt = time.Now()
		})
	}
}

// lookMoveLocked はゲーム画面の視点パッド（加速なし・一定の速さ）。
func (a *App) lookMoveLocked(dx, dy float64) {
	g := a.cfg.Game.LookSens
	a.fx += dx * g
	a.fy += dy * g
	ix, iy := int(a.fx), int(a.fy)
	a.fx -= float64(ix)
	a.fy -= float64(iy)
	if ix != 0 || iy != 0 {
		a.inj.MouseMove(ix, iy)
	}
}

func (a *App) mouseMoveLocked(dx, dy float64) {
	mc := a.cfg.Mouse
	sp := math.Hypot(dx, dy)
	acc := (sp - 1.5) / 6
	if acc < 0 {
		acc = 0
	}
	if acc > 3 {
		acc = 3
	}
	gain := mc.Sensitivity * (1 + mc.Accel*acc)
	a.fx += dx * gain
	a.fy += dy * gain
	ix, iy := int(a.fx), int(a.fy)
	a.fx -= float64(ix)
	a.fy -= float64(iy)
	if ix != 0 || iy != 0 {
		a.inj.MouseMove(ix, iy)
	}
}

func (a *App) scrollLocked(dx, dy float64) {
	mc := a.cfg.Mouse
	k := 3.0 * mc.ScrollSpeed // 40dp でホイール1段(120)
	sign := 1.0
	if mc.ScrollInvert {
		sign = -1
	}
	a.wy += dy * k * sign
	if mc.HScroll {
		a.wx += -dx * k * sign
	}
	step := 120.0
	if mc.SmoothScroll {
		step = 30
	}
	if math.Abs(a.wy) >= step {
		n := int(a.wy/step) * int(step)
		a.inj.Wheel(n, false)
		a.wy -= float64(n)
	}
	if math.Abs(a.wx) >= step {
		n := int(a.wx/step) * int(step)
		a.inj.Wheel(n, true)
		a.wx -= float64(n)
	}
}

// TickLoop はリピートとIME状態の確認。
func (a *App) TickLoop() {
	tk := time.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	for range tk.C {
		a.mu.Lock()
		if a.mode == "keyboard" && a.sess != nil {
			if len(a.kbd.fingers) > 0 {
				a.kbd.Tick(a.now())
				if len(a.kbd.flash) > 0 {
					a.refreshOverlayLocked()
				}
			}
			if time.Since(a.imeAt) > 400*time.Millisecond {
				a.imeAt = time.Now()
				inj := a.inj
				a.mu.Unlock()
				on := inj.IMEOpen() // 他アプリへの問い合わせなのでロックの外で
				a.mu.Lock()
				if on != a.imeOn {
					a.imeOn = on
					if on {
						a.kbd.ResetJaWarn() // IMEをONにした＝日本語に直した
					}
					a.refreshOverlayLocked()
				}
				if fg := a.inj.Foreground(); fg != a.lastFg {
					a.lastFg = fg
					a.kbd.ResetJaWarn() // 別のアプリ・ウィンドウに移った
				}
			}
		}
		a.mu.Unlock()
	}
}

// ===== 設定画面から =====

type Status struct {
	Connected bool      `json:"connected"`
	Calib     Calib     `json:"calib"`
	Phone     PhoneInfo `json:"phone"`
	Mode      string    `json:"mode"`
	TapRight  bool      `json:"tapRight"`
	IPs       []string  `json:"ips"`
	Port      int       `json:"port"`
	PIN       string    `json:"pin"`
	DataDir   string    `json:"dataDir"`
	Version   string    `json:"version"`
}

func (a *App) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Status{
		Connected: a.sess != nil, Calib: a.kbd.Calib, Phone: a.phone, Mode: a.mode,
		TapRight: a.tapRight, IPs: localIPs(), Port: a.cfg.Port, PIN: a.cfg.PIN, DataDir: dataDir(), Version: Version,
	}
}

func (a *App) Config() *Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := *a.cfg
	return &c
}

func (a *App) SetConfig(c *Config) error {
	c.Normalize()
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg = c
	a.applyLocked()
	a.sendCfgLocked()
	a.ov.Update(func(m *OverlayData) { m.PreviewUntil = time.Now().Add(2500 * time.Millisecond) })
	return SaveConfig(c)
}

func (a *App) Layouts() *Layouts {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lay.Clone()
}

func (a *App) SetLayouts(l *Layouts) error {
	l.Normalize()
	l.Schema = layoutSchema
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lay = l
	a.applyLocked()
	a.sendCfgLocked()
	a.ov.Update(func(m *OverlayData) { m.PreviewUntil = time.Now().Add(2500 * time.Millisecond) })
	return SaveLayouts(l)
}

// Preview はPC画面にキーボード表示を数秒出す（位置の確認用）。
func (a *App) Preview() {
	a.ov.Update(func(m *OverlayData) { m.PreviewUntil = time.Now().Add(4 * time.Second) })
}

// TestSound は音を試しに鳴らす（設定画面の試聴）。semi<0 なら左→右の音階で鳴らす。
func (a *App) TestSound(group string, semi int) {
	a.mu.Lock()
	k := a.cfg.Keyboard
	a.mu.Unlock()
	if group == "" {
		group = SndLetter
	}
	if semi >= 0 {
		a.mu.Lock()
		a.sound(group, semi)
		a.mu.Unlock()
		return
	}
	go func() {
		for _, s := range []int{0, 3, 6, 9, 12} {
			a.mu.Lock()
			a.sound(group, s)
			a.mu.Unlock()
			time.Sleep(time.Duration(140+k.ClickVolume*0) * time.Millisecond)
		}
	}()
}

// ===== オーバーレイ表示用モデル =====

type OverlayData struct {
	Cfg          OverlayCfg
	KbVisible    bool
	Board        Board
	Aspect       float64
	Kbd          KbdView
	Flash        map[int]time.Time
	History      string
	IME          bool
	Toast        string
	ToastAt      time.Time
	Zones        []MouseZone
	Echo         string // 画面中央にうっすら表示する、入力したキー
	EchoWarn     bool
	EchoAt       time.Time
	MiniTouch    bool
	MiniX, MiniY float64
	MiniZone     int
	MiniAt       time.Time
	TapRight     bool
	ScrollMode   bool
	Connected    bool
	PreviewUntil time.Time
}

type OverlayModel struct {
	mu  sync.Mutex
	ver uint64
	d   OverlayData
}

func (o *OverlayModel) Update(f func(m *OverlayData)) {
	o.mu.Lock()
	f(&o.d)
	o.ver++
	o.mu.Unlock()
}

// Get は描画用のコピーを返す。
func (o *OverlayModel) Get() (OverlayData, uint64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	d := o.d
	if o.d.Flash != nil {
		d.Flash = map[int]time.Time{}
		for k, v := range o.d.Flash {
			if time.Since(v) < time.Second {
				d.Flash[k] = v
			} else {
				delete(o.d.Flash, k)
			}
		}
	}
	return d, o.ver
}

// dirAdapter は方向入力の出力を App につなぐ（App のロック中に呼ばれる）。
type dirAdapter struct{ a *App }

func (o dirAdapter) typeASCII(s string)           { o.a.inj.TypeASCII(s) }
func (o dirAdapter) key(mods []uint16, vk uint16) { o.a.inj.KeyCombo(mods, vk) }
func (o dirAdapter) uni(s string) {
	for _, r := range s {
		o.a.inj.TypeUnicode(r)
	}
}
func (o dirAdapter) setIME(on bool) {
	o.a.inj.SetIME(on, on)
	o.a.imeAt = time.Time{} // IMEの状態をすぐ読み直す
}
func (o dirAdapter) snd(group string, semi int) { o.a.sound(group, semi) }
func (o dirAdapter) echo(s string, warn bool)   { o.a.echo(s, warn) }
func (o dirAdapter) toast(s string)             { o.a.toast(s) }
func (o dirAdapter) sub(m string)               { o.a.send(map[string]any{"t": "dsub", "m": m}) }
