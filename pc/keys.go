package main

import "strings"

// Injector はOSへの入力送信（Windowsでは SendInput）。
type Injector interface {
	KeyCombo(mods []uint16, vk uint16)
	KeyDown(vk uint16)
	KeyUp(vk uint16)
	TypeASCII(s string) // 現在のキーボード配列に合わせたキー入力
	TypeUnicode(r rune) // Unicode文字を直接送る
	IMEOpen() bool      // 前面のアプリでIME(日本語入力)がONか
	MouseMove(dx, dy int)
	MouseButton(btn int, down bool) // 0:左 1:右 2:中
	Wheel(delta int, horizontal bool)
	SetIME(open bool, kana bool)
	Foreground() uintptr // 前面のウィンドウ（アプリの切り替えを知るため）
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
	"KANJI": 0x19, "HANKAKU": 0x19, "CONVERT": 0x1C, "NONCONVERT": 0x1D, "KANA": 0xF2,
	"MUTE": 0xAD, "VOLDOWN": 0xAE, "VOLUP": 0xAF, "NEXT": 0xB0, "PREV": 0xB1, "PLAYPAUSE": 0xB3,
	"CAPSLOCK": 0x14, "EISU": 0xF0, "WIN": 0x5B,
	// JIS(日本語)配列の記号キー
	"MINUS": 0xBD, "CARET": 0xDE, "YEN": 0xDC, "AT": 0xC0, "LBRACKET": 0xDB, "RBRACKET": 0xDD,
	"SEMICOLON": 0xBB, "COLON": 0xBA, "COMMA": 0xBC, "PERIOD": 0xBE, "SLASH": 0xBF, "BACKSLASH": 0xE2,
	"PLUS": 0xBB,
	// 修飾キー（左側）
	"LSHIFT": 0xA0, "LCTRL": 0xA2, "LALT": 0xA4,
}

// modVK は修飾キー名から仮想キーコード。
var modVK = map[string]uint16{"shift": 0xA0, "ctrl": 0xA2, "alt": 0xA4, "win": 0x5B}

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
