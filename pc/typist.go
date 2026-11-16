package main

import (
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Injector はOSへの入力送信（Windowsでは SendInput）。
type Injector interface {
	KeyCombo(mods []uint16, vk uint16)
	KeyDown(vk uint16)
	KeyUp(vk uint16)
	TypeASCII(s string) // 現在のキーボード配列に合わせたキー入力
	TypeUnicode(r rune) // Unicode文字を直接送る
	MouseMove(dx, dy int)
	MouseButton(btn int, down bool) // 0:左 1:右 2:中
	Wheel(delta int, horizontal bool)
	SetIME(open bool, kana bool)
}

const (
	vkBack    = 0x08
	vkTab     = 0x09
	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkEscape  = 0x1B
	vkSpace   = 0x20
	vkLWin    = 0x5B
)

var keyNames = map[string]uint16{
	"BS": vkBack, "BACKSPACE": vkBack, "TAB": vkTab, "ENTER": vkReturn, "RETURN": vkReturn,
	"ESC": vkEscape, "ESCAPE": vkEscape, "SPACE": vkSpace,
	"PGUP": 0x21, "PAGEUP": 0x21, "PGDN": 0x22, "PAGEDOWN": 0x22,
	"END": 0x23, "HOME": 0x24, "LEFT": 0x25, "UP": 0x26, "RIGHT": 0x27, "DOWN": 0x28,
	"PRINTSCREEN": 0x2C, "INS": 0x2D, "INSERT": 0x2D, "DEL": 0x2E, "DELETE": 0x2E,
	"APPS": 0x5D, "MENU": 0x5D,
	"KANJI": 0x19, "HANKAKU": 0x19, "CONVERT": 0x1C, "NONCONVERT": 0x1D, "KANA": 0x15,
	"MUTE": 0xAD, "VOLDOWN": 0xAE, "VOLUP": 0xAF, "NEXT": 0xB0, "PREV": 0xB1, "PLAYPAUSE": 0xB3,
	"CAPSLOCK": 0x14, "MINUS": 0xBD, "PLUS": 0xBB, "COMMA": 0xBC, "PERIOD": 0xBE,
}

var modNames = map[string]uint16{
	"CTRL": vkControl, "CONTROL": vkControl, "SHIFT": vkShift, "ALT": vkMenu, "WIN": vkLWin,
}

// IsAction は "{XXX}" 形式のアクション文字列か判定する。
func IsAction(s string) bool {
	return len(s) > 2 && s[0] == '{' && s[len(s)-1] == '}'
}

func actionName(s string) string {
	return strings.ToUpper(strings.TrimSpace(s[1 : len(s)-1]))
}

// ParseCombo は "CTRL+SHIFT+Z" などを解釈する。
func ParseCombo(name string) (mods []uint16, vk uint16, ok bool) {
	parts := strings.Split(name, "+")
	for i, p := range parts {
		p = strings.TrimSpace(p)
		if i < len(parts)-1 {
			m, ok := modNames[p]
			if !ok {
				return nil, 0, false
			}
			mods = append(mods, m)
			continue
		}
		if v, ok := keyNames[p]; ok {
			return mods, v, true
		}
		if len(p) >= 2 && p[0] == 'F' {
			n := 0
			for _, c := range p[1:] {
				if c < '0' || c > '9' {
					n = -1
					break
				}
				n = n*10 + int(c-'0')
			}
			if n >= 1 && n <= 24 {
				return mods, uint16(0x70 + n - 1), true
			}
		}
		if len(p) == 1 {
			c := p[0]
			if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				return mods, uint16(c), true
			}
		}
	}
	return nil, 0, false
}

// Typist はフリックで確定した文字やアクションをPCへの入力に変換する。
type Typist struct {
	mu       sync.Mutex
	inj      Injector
	cfg      KeyboardCfg
	kind     string // kana / alpha / num
	last     rune
	lastOK   bool
	caps     bool
	history  []rune
	imeState int // -1 不明, 0 off, 1 on（このアプリが最後に設定した状態）
}

func NewTypist(inj Injector) *Typist {
	return &Typist{inj: inj, kind: "kana", imeState: -1}
}

func (t *Typist) Configure(cfg KeyboardCfg) {
	t.mu.Lock()
	t.cfg = cfg
	t.mu.Unlock()
}

// SetKind はキーボード種別の切替。IME制御が有効ならIMEのON/OFFも切り替える。
func (t *Typist) SetKind(kind string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.kind = kind
	t.lastOK = false
	if t.cfg.ImeControl {
		t.ensureIME()
	}
}

func (t *Typist) wantIME() bool {
	return t.kind == "kana" && t.cfg.KanaOutput == "ime"
}

func (t *Typist) ensureIME() {
	if !t.cfg.ImeControl {
		return
	}
	on := t.wantIME()
	t.inj.SetIME(on, on)
}

func (t *Typist) History() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	h := t.history
	if len(h) > 40 {
		h = h[len(h)-40:]
	}
	return string(h)
}

func (t *Typist) ClearHistory() {
	t.mu.Lock()
	t.history = t.history[:0]
	t.lastOK = false
	t.mu.Unlock()
}

func (t *Typist) pushHist(r rune) {
	t.history = append(t.history, r)
	if len(t.history) > 200 {
		t.history = append([]rune{}, t.history[100:]...)
	}
}

func (t *Typist) popHist() {
	if len(t.history) > 0 {
		t.history = t.history[:len(t.history)-1]
	}
}

func (t *Typist) typeRune(r rune) {
	if t.caps && r < 128 && unicode.IsLower(r) {
		r = unicode.ToUpper(r)
	}
	if t.wantIME() {
		if s, ok := romaji[r]; ok {
			t.inj.TypeASCII(s)
		} else if r < 128 {
			t.inj.TypeASCII(string(r))
		} else {
			t.inj.TypeUnicode(r)
		}
	} else if r < 128 && unicode.IsPrint(r) {
		t.inj.TypeASCII(string(r))
	} else {
		t.inj.TypeUnicode(r)
	}
	t.pushHist(r)
	t.last = r
	t.lastOK = true
}

// Commit はフリックで選ばれた出力を処理する（文字列 or アクション）。
// アプリ側で処理しなかったアクションのみ受け取る。
func (t *Typist) Commit(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !IsAction(s) {
		if t.cfg.ImeControl {
			t.ensureIME()
		}
		for len(s) > 0 {
			r, n := utf8.DecodeRuneInString(s)
			s = s[n:]
			t.typeRune(r)
		}
		return
	}
	name := actionName(s)
	switch name {
	case "CYCLE", "DAKU", "HANDAKU", "SMALL", "CASE":
		if !t.lastOK {
			return
		}
		var nr rune
		switch name {
		case "CYCLE", "CASE":
			nr = cycleRune(t.last)
		case "DAKU":
			nr = dakuRune(t.last)
		case "HANDAKU":
			nr = handakuRune(t.last)
		case "SMALL":
			nr = smallRune(t.last)
		}
		if nr == t.last {
			return
		}
		t.inj.KeyCombo(nil, vkBack)
		t.popHist()
		caps := t.caps
		t.caps = false
		t.typeRune(nr)
		t.caps = caps
		return
	case "CAPS":
		t.caps = !t.caps
		return
	case "IME_ON":
		t.inj.SetIME(true, true)
		return
	case "IME_OFF":
		t.inj.SetIME(false, false)
		return
	case "IME_TOGGLE":
		t.inj.KeyCombo(nil, 0x19)
		return
	case "NONE", "":
		return
	}
	mods, vk, ok := ParseCombo(name)
	if !ok {
		return
	}
	t.inj.KeyCombo(mods, vk)
	switch {
	case vk == vkBack && len(mods) == 0:
		t.popHist()
		t.lastOK = false
	case vk == vkReturn || vk == vkEscape:
		t.history = append(t.history, '⏎')
		if vk == vkEscape {
			t.history[len(t.history)-1] = '⎋'
		}
		t.lastOK = false
	case vk == vkSpace && len(mods) == 0:
		t.pushHist('␣')
		t.lastOK = false
	default:
		t.lastOK = false
	}
}

func (t *Typist) Caps() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.caps
}
