package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/draw"
	"image/jpeg"
	"log"
	"math"
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
	T    string  `json:"t"`
	A    string  `json:"a"`
	B    string  `json:"b"`
	K    string  `json:"k"`
	S    string  `json:"s"`
	M    string  `json:"m"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Dx   float64 `json:"dx"`
	Dy   float64 `json:"dy"`
	N    int     `json:"n"`
	Z    int     `json:"z"`
	Wdp  float64 `json:"wdp"`
	Hdp  float64 `json:"hdp"`
	Name string  `json:"name"`
	Pin  string  `json:"pin"`
	V    int     `json:"v"`
	Ts   int64   `json:"ts"` // スマホ側のタッチ時刻(ms)
	Text string  `json:"text"`
	Jpg  string  `json:"jpg"`
}

type App struct {
	mu         sync.Mutex
	cfg        *Config
	lay        *Layouts
	inj        Injector
	eng        *Engine
	typ        *Typist
	ov         *OverlayModel
	sess       Sender
	phone      PhoneInfo
	mode       string // mouse / keyboard
	kbIndex    int
	tapRight   bool
	scrollMode bool
	held       map[int]bool
	fx, fy     float64
	wy, wx     float64
	webURL     string
	tsOff      int64
	start      time.Time
	OpenURL    func(string)
}

func NewApp(cfg *Config, lay *Layouts, inj Injector) *App {
	a := &App{
		cfg:   cfg,
		lay:   lay,
		inj:   inj,
		typ:   NewTypist(inj),
		ov:    &OverlayModel{},
		mode:  "mouse",
		held:  map[int]bool{},
		phone: PhoneInfo{Wdp: 400, Hdp: 860},
		start: time.Now(),
	}
	a.eng = NewEngine(a.onEngine)
	a.eng.Clock = a.now
	a.applyLocked()
	return a
}

func (a *App) now() int64 { return time.Since(a.start).Milliseconds() }

func (a *App) curKb() *KbLayout {
	if a.kbIndex < 0 || a.kbIndex >= len(a.lay.Keyboards) {
		a.kbIndex = 0
	}
	return &a.lay.Keyboards[a.kbIndex]
}

// applyLocked は設定・配列の変更をエンジンとオーバーレイへ反映する。
func (a *App) applyLocked() {
	kb := a.curKb()
	a.eng.Configure(a.cfg.Keyboard, kb, a.lay.Region, a.phone.Wdp, a.phone.Hdp)
	a.typ.Configure(a.cfg.Keyboard)
	a.refreshOverlayLocked()
}

func (a *App) refreshOverlayLocked() {
	kb := *a.curKb()
	aspect := 1.0
	if a.lay.Region.H > 0 {
		aspect = (a.lay.Region.W * a.phone.Wdp) / (a.lay.Region.H * a.phone.Hdp)
	}
	snap := a.eng.Snap()
	hist := a.typ.History()
	caps := a.typ.Caps()
	a.ov.Update(func(m *OverlayData) {
		m.Cfg = a.cfg.Overlay
		m.KbStyle = a.cfg.Keyboard.Style
		m.Input = a.cfg.Keyboard.Input
		m.MirrorScale = a.cfg.Keyboard.MirrorScale
		if !a.cfg.Keyboard.Mirror {
			m.Frame = nil
		}
		m.KbVisible = a.mode == "keyboard" && a.sess != nil
		m.Layout = kb
		m.Aspect = aspect
		m.Snap = snap
		m.Title = kb.Title
		m.History = hist
		m.Caps = caps
		m.Zones = a.lay.MouseZones
		m.PhoneAspect = a.phone.Wdp / math.Max(a.phone.Hdp, 1)
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
	case "arm":
		ms = h.ArmMs
	case "dir":
		ms = h.DirMs
	case "commit":
		ms = h.CommitMs
	case "toggle":
		ms = h.ToggleMs
	case "cancel":
		if h.HoverMs > 0 {
			a.send(map[string]any{"t": "vib", "pat": []int{0, 12, 60, 12}, "amp": h.Amp})
		}
		return
	case "double":
		if h.ToggleMs > 0 {
			a.send(map[string]any{"t": "vib", "pat": []int{0, h.ToggleMs, 80, h.ToggleMs}, "amp": h.Amp})
		}
		return
	}
	if ms > 0 {
		a.send(map[string]any{"t": "vib", "ms": ms, "amp": h.Amp})
	}
}

// エンジンからのイベント（App のロック中に呼ばれる）
func (a *App) onEngine(ev EngineEvent) {
	switch ev.Kind {
	case "haptic":
		a.vib(ev.Haptic)
	case "commit":
		a.doActionLocked(ev.Text, "tap")
		a.ov.Update(func(m *OverlayData) {
			m.Flash = ev.Text
			if IsAction(ev.Text) {
				m.Flash = actionLabel(ev.Text)
			}
			m.FlashAt = time.Now()
		})
	}
}

func actionLabel(s string) string {
	n := actionName(s)
	labels := map[string]string{
		"BS": "削除", "ENTER": "改行", "SPACE": "空白", "LEFT": "←", "RIGHT": "→", "UP": "↑", "DOWN": "↓",
		"CYCLE": "゛゜小", "DAKU": "゛", "HANDAKU": "゜", "SMALL": "小", "CASE": "a/A", "CAPS": "大文字",
		"MODE_NEXT": "切替", "MOUSE": "マウス", "KEYBOARD": "キーボード", "TAB": "Tab", "ESC": "Esc", "DEL": "Del",
		"HOME": "先頭", "END": "末尾", "PGUP": "PgUp", "PGDN": "PgDn",
		"CTRL+Z": "戻す", "CTRL+C": "コピー", "CTRL+V": "貼付", "CTRL+X": "切取", "CTRL+A": "全選択", "CTRL+BS": "単語削除",
		"SHIFT+ENTER": "Shift改行", "CTRL+ENTER": "Ctrl改行", "SHIFT+TAB": "Shift+Tab",
		"F6": "ひら", "F7": "カナ", "F8": "半角", "F10": "英数",
		"MODE:KANA": "かな", "MODE:ALPHA": "ABC", "MODE:NUM": "123",
		"CLICK_L": "左クリック", "CLICK_R": "右クリック", "CLICK_M": "中クリック", "TAPMODE": "左右切替", "SCROLLMODE": "スクロール",
	}
	if l, ok := labels[n]; ok {
		return l
	}
	return n
}

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

// doActionLocked はアクション/文字を実行する。phase: tap / down / up
func (a *App) doActionLocked(s string, phase string) {
	if !IsAction(s) {
		if phase != "up" {
			a.typ.Commit(s)
		}
		return
	}
	name := actionName(s)
	if btn, ok := isClickAction(name); ok {
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
	switch {
	case name == "MOUSE":
		a.setModeLocked("mouse")
	case name == "KEYBOARD":
		a.setModeLocked("keyboard")
	case name == "TOGGLE":
		if a.mode == "mouse" {
			a.setModeLocked("keyboard")
		} else {
			a.setModeLocked("mouse")
		}
	case name == "MODE_NEXT":
		a.setKbLocked((a.kbIndex + 1) % len(a.lay.Keyboards))
	case strings.HasPrefix(name, "MODE:"):
		want := strings.ToLower(strings.TrimPrefix(name, "MODE:"))
		for i := range a.lay.Keyboards {
			if strings.ToLower(a.lay.Keyboards[i].Name) == want {
				a.setKbLocked(i)
				break
			}
		}
	case name == "TAPMODE":
		a.tapRight = !a.tapRight
		a.vib("double")
		if a.tapRight {
			a.toast("タップ = 右クリック")
		} else {
			a.toast("タップ = 左クリック")
		}
		a.sendStateLocked()
	case name == "SCROLLMODE":
		a.scrollMode = !a.scrollMode
		a.vib("double")
		if a.scrollMode {
			a.toast("パッド = スクロール")
		} else {
			a.toast("パッド = カーソル移動")
		}
		a.sendStateLocked()
	case name == "DBLCLICK":
		for i := 0; i < 2; i++ {
			a.inj.MouseButton(0, true)
			a.inj.MouseButton(0, false)
		}
	case name == "SETTINGS":
		if a.OpenURL != nil && a.webURL != "" {
			go a.OpenURL(a.webURL)
		}
	case name == "CLEAR":
		a.typ.ClearHistory()
	default:
		a.typ.Commit(s)
	}
	a.refreshOverlayLocked()
}

func (a *App) setKbLocked(i int) {
	if i < 0 || i >= len(a.lay.Keyboards) {
		return
	}
	a.kbIndex = i
	kb := a.curKb()
	a.eng.Configure(a.cfg.Keyboard, kb, a.lay.Region, a.phone.Wdp, a.phone.Hdp)
	a.typ.SetKind(kb.Kind)
	a.vib("toggle")
	a.toast("キーボード: " + kb.Title)
	a.sendCfgLocked()
	a.refreshOverlayLocked()
}

func (a *App) setModeLocked(m string) {
	if m != "mouse" && m != "keyboard" {
		return
	}
	a.mode = m
	a.eng.Cancel()
	a.releaseHeldLocked()
	if m == "keyboard" && a.gboard() {
		a.typ.SetKind("direct") // PCのIMEはOFFに（変換はスマホ側で済ませる）
		a.ov.Update(func(o *OverlayData) { o.Comp = "" })
		a.toast("キーボード（スマホ）")
	} else if m == "keyboard" {
		a.typ.SetKind(a.curKb().Kind)
		a.toast("キーボード: " + a.curKb().Title)
	} else {
		a.toast("マウス")
	}
	a.vib("toggle")
	a.send(map[string]any{"t": "setmode", "m": m})
	a.refreshOverlayLocked()
}

func (a *App) gboard() bool { return a.cfg.Keyboard.Input == "gboard" }

func (a *App) releaseHeldLocked() {
	for b := range a.held {
		a.inj.MouseButton(b, false)
	}
	a.held = map[int]bool{}
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
	kb := a.curKb()
	keys := make([]keyView, 0, len(kb.Keys))
	for _, k := range kb.Keys {
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
		"kb": map[string]any{
			"input": a.cfg.Keyboard.Input, "mirror": a.cfg.Keyboard.Mirror, "mirrorFps": a.cfg.Keyboard.MirrorFps,
			"mirrorWidth": a.cfg.Keyboard.MirrorWidth, "mirrorQuality": a.cfg.Keyboard.MirrorQuality,
			"title": kb.Title, "cols": kb.Cols, "rows": kb.Rows, "keys": keys, "region": a.lay.Region,
		},
		"haptic": a.cfg.Haptic.Enabled,
	})
	a.sendStateLocked()
}

// ===== 接続 =====

func (a *App) OnConnect(s Sender, info PhoneInfo) {
	a.mu.Lock()
	defer a.mu.Unlock()
	old := a.sess
	a.sess = s
	if old != nil && old != s {
		old.Close()
	}
	if info.Wdp > 0 && info.Hdp > 0 {
		a.phone = info
	} else {
		a.phone.Name = info.Name
	}
	a.mode = "mouse"
	a.eng.Cancel()
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
	a.eng.Cancel()
	a.releaseHeldLocked()
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
	case "kt": // キーボードモードのタッチ
		if a.mode != "keyboard" {
			return
		}
		// スマホ側の時刻を使う（Wi-Fiの遅延の揺れで速さの判定が狂わないように）
		if m.Ts > 0 {
			if m.A == "d" || a.tsOff == 0 {
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
			a.eng.Down(m.X, m.Y, t)
		case "m":
			a.eng.Move(m.X, m.Y, t)
		case "u":
			a.eng.Up(m.X, m.Y, t)
		default:
			a.eng.Cancel()
		}
		a.refreshOverlayLocked()
	case "mm":
		a.mouseMoveLocked(m.Dx, m.Dy)
	case "ms":
		a.scrollLocked(m.Dx, m.Dy)
	case "tap":
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
			} else if m.S == "up" {
				a.doActionLocked(m.A, "tap")
			}
		}
	case "act":
		a.doActionLocked(m.A, "tap")
	case "hw": // 音量キー
		act := ""
		up := m.K == "volup"
		if a.mode == "keyboard" {
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
			a.applyLocked()
		}
	case "comp": // スマホのキーボードで変換中の文字
		a.ov.Update(func(o *OverlayData) { o.Comp = m.Text })
	case "text": // スマホのキーボードで確定した文字
		a.ov.Update(func(o *OverlayData) { o.Comp = "" })
		if m.Text != "" {
			a.typ.CommitDirect(m.Text)
			a.ov.Update(func(o *OverlayData) {
				o.Flash = strings.ReplaceAll(m.Text, "\n", "⏎")
				o.FlashAt = time.Now()
			})
			a.refreshOverlayLocked()
		}
	case "key": // スマホのキーボードの特殊キー（BS/ENTER/矢印など）
		if _, _, ok := ParseCombo(strings.ToUpper(m.K)); ok {
			a.typ.Commit("{" + strings.ToUpper(m.K) + "}")
			a.refreshOverlayLocked()
		}
	case "ping":
		a.send(map[string]any{"t": "pong"})
	}
}

// Frame はスマホから届いたキーボード画面（BGRA、上から下）。
type Frame struct {
	W, H int
	BGRA []byte
	At   time.Time
}

// HandleFrame はキーボード画面のJPEGを展開してPC画面表示用に保持する（Appのロックの外で呼ぶ）。
func (a *App) HandleFrame(s Sender, m *Msg) {
	a.mu.Lock()
	ok := a.sess == s && a.mode == "keyboard" && a.cfg.Keyboard.Mirror
	a.mu.Unlock()
	if !ok || m.Jpg == "" {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(m.Jpg)
	if err != nil {
		return
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return
	}
	b := img.Bounds()
	rgba := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(rgba, rgba.Bounds(), img, b.Min, draw.Src)
	px := rgba.Pix
	for i := 0; i+3 < len(px); i += 4 {
		px[i], px[i+2] = px[i+2], px[i] // RGBA → BGRA
	}
	f := &Frame{W: b.Dx(), H: b.Dy(), BGRA: px, At: time.Now()}
	a.ov.Update(func(o *OverlayData) { o.Frame = f })
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

// TickLoop は確定待機・リピートの時間処理。
func (a *App) TickLoop() {
	tk := time.NewTicker(10 * time.Millisecond)
	defer tk.Stop()
	for range tk.C {
		a.mu.Lock()
		if a.mode == "keyboard" && a.eng.state != stIdle {
			before := a.eng.Snap()
			a.eng.Tick(a.now())
			if a.eng.Snap() != before {
				a.refreshOverlayLocked()
			}
		}
		a.mu.Unlock()
	}
}

// ===== 設定画面から =====

type Status struct {
	Connected bool      `json:"connected"`
	Phone     PhoneInfo `json:"phone"`
	Mode      string    `json:"mode"`
	Keyboard  string    `json:"keyboard"`
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
		Connected: a.sess != nil, Phone: a.phone, Mode: a.mode, Keyboard: a.curKb().Title,
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
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lay = l
	a.applyLocked()
	a.sendCfgLocked()
	a.ov.Update(func(m *OverlayData) { m.PreviewUntil = time.Now().Add(2500 * time.Millisecond) })
	return SaveLayouts(l)
}

func (a *App) Preview(name string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if name != "" {
		for i := range a.lay.Keyboards {
			if a.lay.Keyboards[i].Name == name {
				a.kbIndex = i
				a.applyLocked()
			}
		}
	}
	a.ov.Update(func(m *OverlayData) { m.PreviewUntil = time.Now().Add(4 * time.Second) })
}

// ===== オーバーレイ表示用モデル =====

type OverlayData struct {
	Cfg          OverlayCfg
	KbStyle      string
	Input        string
	Comp         string
	Frame        *Frame // スマホのキーボード画面（最新）
	MirrorScale  float64
	KbVisible    bool
	Layout       KbLayout
	Aspect       float64
	Snap         EngineSnap
	Title        string
	History      string
	Caps         bool
	Flash        string
	FlashAt      time.Time
	Toast        string
	ToastAt      time.Time
	Zones        []MouseZone
	PhoneAspect  float64
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
	return o.d, o.ver
}
