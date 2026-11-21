package main

import (
	"encoding/json"
	"strings"
)

// BKey はキーボードのキー1つ。座標はキー1個分=1の単位（横15×縦5.7）。
type BKey struct {
	Label   string  `json:"label"`             // 表示
	Shift   string  `json:"shift,omitempty"`   // Shift時の表示
	FnLabel string  `json:"fnLabel,omitempty"` // Fn時の表示
	Key     string  `json:"key,omitempty"`     // 送るキー名（A, 1, MINUS, ENTER, ...）
	FnKey   string  `json:"fnKey,omitempty"`   // Fnを押している時に送るキー名
	Mod     string  `json:"mod,omitempty"`     // 修飾キー: shift / ctrl / alt / win / fn
	Action  string  `json:"action,omitempty"`  // {MOUSE} などのアクション
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	W       float64 `json:"w"`
	H       float64 `json:"h"`
	Repeat  bool    `json:"repeat,omitempty"` // 押し続けるとリピート
}

// Letter は入力補助に使う英字（英字キーでなければ 0）。
func (k *BKey) Letter() rune {
	if len(k.Key) == 1 && k.Key[0] >= 'A' && k.Key[0] <= 'Z' && k.Mod == "" {
		return rune(k.Key[0] - 'A' + 'a')
	}
	return 0
}

// Board はスマホ画面全体（横向き）に並べるキーボード。
type Board struct {
	Cols float64 `json:"cols"`
	Rows float64 `json:"rows"`
	Keys []BKey  `json:"keys"`
}

// MouseZone はマウスモード時のスマホ画面の領域。
// Type: pad(カーソル移動) / scroll(スクロール) / button(アクション)
type MouseZone struct {
	Type   string  `json:"type"`
	Label  string  `json:"label"`
	Action string  `json:"action,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
}

// layoutSchema は配列データの版。上げると古い配列を新しい既定配列に置き換える（旧配列は layouts.old.json に退避）。
const layoutSchema = 3

type Layouts struct {
	Schema     int         `json:"schema"`
	Board      Board       `json:"board"`
	MouseZones []MouseZone `json:"mouseZones"`
	GameZones  []MouseZone `json:"gameZones"` // ゲーム画面（ボタン＝キーやクリックを押しっぱなし、look＝視点操作）
}

func (ls *Layouts) Clone() *Layouts {
	b, _ := json.Marshal(ls)
	var c Layouts
	_ = json.Unmarshal(b, &c)
	return &c
}

// Normalize は壊れた値を補正する。
func (ls *Layouts) Normalize() {
	clamp := func(v, lo, hi float64) float64 {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	if len(ls.Board.Keys) == 0 || ls.Board.Cols <= 0 || ls.Board.Rows <= 0 {
		ls.Board = DefaultLayouts().Board
	}
	for i := range ls.Board.Keys {
		k := &ls.Board.Keys[i]
		if k.W <= 0 {
			k.W = 1
		}
		if k.H <= 0 {
			k.H = 1
		}
		k.Mod = strings.ToLower(k.Mod)
		k.Key = strings.ToUpper(k.Key)
		k.FnKey = strings.ToUpper(k.FnKey)
	}
	if len(ls.GameZones) == 0 {
		ls.GameZones = DefaultLayouts().GameZones
	}
	for _, zs := range [][]MouseZone{ls.MouseZones, ls.GameZones} {
		for i := range zs {
			z := &zs[i]
			z.X = clamp(z.X, 0, 1)
			z.Y = clamp(z.Y, 0, 1)
			z.W = clamp(z.W, 0.01, 1)
			z.H = clamp(z.H, 0.01, 1)
			if z.Type != "pad" && z.Type != "scroll" && z.Type != "button" && z.Type != "look" {
				z.Type = "pad"
			}
		}
	}
}

// topBar は最上段（モード切替ボタンと、指を逃がして取り消すための空き）の高さ。
const topBar = 0.7

func DefaultLayouts() *Layouts {
	var keys []BKey
	add := func(k BKey) { keys = append(keys, k) }
	row := func(r int) float64 { return topBar + float64(r) }
	// 最上段
	add(BKey{Label: "マウス", Action: "{MOUSE}", X: 0, Y: 0, W: 2, H: topBar})
	add(BKey{Label: "Del", Key: "DEL", X: 13.5, Y: 0, W: 1.5, H: topBar, Repeat: true})

	// 1段目: Esc 1〜0 - ^ ¥ BS（Fnで F1〜F12）
	add(BKey{Label: "Esc", Key: "ESC", X: 0, Y: row(0), W: 1, H: 1})
	nums := []struct{ l, s string }{{"1", "!"}, {"2", "\""}, {"3", "#"}, {"4", "$"}, {"5", "%"}, {"6", "&"}, {"7", "'"}, {"8", "("}, {"9", ")"}, {"0", ""}}
	for i, n := range nums {
		f := "F" + itoa(i+1)
		add(BKey{Label: n.l, Shift: n.s, Key: n.l, FnKey: f, FnLabel: f, X: float64(1 + i), Y: row(0), W: 1, H: 1})
	}
	add(BKey{Label: "-", Shift: "=", Key: "MINUS", FnKey: "F11", FnLabel: "F11", X: 11, Y: row(0), W: 1, H: 1})
	add(BKey{Label: "^", Shift: "~", Key: "CARET", FnKey: "F12", FnLabel: "F12", X: 12, Y: row(0), W: 1, H: 1})
	add(BKey{Label: "¥", Shift: "|", Key: "YEN", FnKey: "PRINTSCREEN", FnLabel: "PrtSc", X: 13, Y: row(0), W: 1, H: 1})
	add(BKey{Label: "BS", Key: "BS", FnKey: "DEL", FnLabel: "Del", X: 14, Y: row(0), W: 1, H: 1, Repeat: true})

	// 2段目: Tab Q〜P @ [ Enter(2段)
	add(BKey{Label: "Tab", Key: "TAB", X: 0, Y: row(1), W: 1.75, H: 1})
	for i, c := range "QWERTYUIOP" {
		add(BKey{Label: string(c), Key: string(c), X: 1.75 + float64(i), Y: row(1), W: 1, H: 1})
	}
	add(BKey{Label: "@", Shift: "`", Key: "AT", X: 11.75, Y: row(1), W: 1, H: 1})
	add(BKey{Label: "[", Shift: "{", Key: "LBRACKET", X: 12.75, Y: row(1), W: 1, H: 1})
	add(BKey{Label: "Enter", Key: "ENTER", X: 13.75, Y: row(1), W: 1.25, H: 2})

	// 3段目: Ctrl A〜L ; : ]（Fn+Ctrl = CapsLock）
	add(BKey{Label: "Ctrl", Mod: "ctrl", FnKey: "CAPSLOCK", FnLabel: "Caps", X: 0, Y: row(2), W: 1.75, H: 1})
	for i, c := range "ASDFGHJKL" {
		add(BKey{Label: string(c), Key: string(c), X: 1.75 + float64(i), Y: row(2), W: 1, H: 1})
	}
	add(BKey{Label: ";", Shift: "+", Key: "SEMICOLON", X: 10.75, Y: row(2), W: 1, H: 1})
	add(BKey{Label: ":", Shift: "*", Key: "COLON", X: 11.75, Y: row(2), W: 1, H: 1})
	add(BKey{Label: "]", Shift: "}", Key: "RBRACKET", X: 12.75, Y: row(2), W: 1, H: 1})

	// 4段目: Shift Z〜M , . / \ ↑ Shift
	add(BKey{Label: "Shift", Mod: "shift", X: 0, Y: row(3), W: 2, H: 1})
	for i, c := range "ZXCVBNM" {
		add(BKey{Label: string(c), Key: string(c), X: 2 + float64(i), Y: row(3), W: 1, H: 1})
	}
	add(BKey{Label: ",", Shift: "<", Key: "COMMA", X: 9, Y: row(3), W: 1, H: 1})
	add(BKey{Label: ".", Shift: ">", Key: "PERIOD", X: 10, Y: row(3), W: 1, H: 1})
	add(BKey{Label: "/", Shift: "?", Key: "SLASH", X: 11, Y: row(3), W: 1, H: 1})
	add(BKey{Label: "\\", Shift: "_", Key: "BACKSLASH", X: 12, Y: row(3), W: 1, H: 1})
	add(BKey{Label: "↑", Key: "UP", FnKey: "PGUP", FnLabel: "PgUp", X: 13, Y: row(3), W: 1, H: 1, Repeat: true})
	add(BKey{Label: "Shift", Mod: "shift", X: 14, Y: row(3), W: 1, H: 1})

	// 5段目: Fn 半/全 Win Alt 無変換 Space 変換 かな ← ↓ →
	add(BKey{Label: "Fn", Mod: "fn", X: 0, Y: row(4), W: 1, H: 1})
	add(BKey{Label: "半/全", Key: "KANJI", X: 1, Y: row(4), W: 1, H: 1})
	add(BKey{Label: "Win", Mod: "win", FnKey: "APPS", FnLabel: "Menu", X: 2, Y: row(4), W: 1, H: 1})
	add(BKey{Label: "Alt", Mod: "alt", X: 3, Y: row(4), W: 1, H: 1})
	add(BKey{Label: "無変換", Key: "NONCONVERT", X: 4, Y: row(4), W: 1.25, H: 1})
	add(BKey{Label: "Space", Key: "SPACE", X: 5.25, Y: row(4), W: 4.5, H: 1})
	add(BKey{Label: "変換", Key: "CONVERT", X: 9.75, Y: row(4), W: 1.25, H: 1})
	add(BKey{Label: "かな", Key: "KANA", X: 11, Y: row(4), W: 1, H: 1})
	add(BKey{Label: "←", Key: "LEFT", FnKey: "HOME", FnLabel: "Home", X: 12, Y: row(4), W: 1, H: 1, Repeat: true})
	add(BKey{Label: "↓", Key: "DOWN", FnKey: "PGDN", FnLabel: "PgDn", X: 13, Y: row(4), W: 1, H: 1, Repeat: true})
	add(BKey{Label: "→", Key: "RIGHT", FnKey: "END", FnLabel: "End", X: 14, Y: row(4), W: 1, H: 1, Repeat: true})

	return &Layouts{
		Schema: layoutSchema,
		Board:  Board{Cols: 15, Rows: topBar + 5, Keys: keys},
		MouseZones: []MouseZone{
			// 左手の親指でボタン、右手の親指でカーソル（ノートPCのタッチパッド風）
			{Type: "button", Label: "キーボード", Action: "{KEYBOARD}", X: 0, Y: 0, W: 0.16, H: 0.14},
			{Type: "button", Label: "左クリック", Action: "{CLICK_L}", X: 0, Y: 0.14, W: 0.16, H: 0.43},
			{Type: "button", Label: "右クリック", Action: "{CLICK_R}", X: 0, Y: 0.57, W: 0.16, H: 0.43},
			{Type: "pad", Label: "パッド", X: 0.16, Y: 0, W: 0.72, H: 1},
			{Type: "scroll", Label: "スクロール", X: 0.88, Y: 0, W: 0.12, H: 1},
		},
		GameZones: defaultGameZones(),
	}
}

// defaultGameZones はゲーム画面の既定配置。左手でキー（WASD）、右手で視点、右端に射撃・構え。
func defaultGameZones() []MouseZone {
	z := []MouseZone{
		{Type: "button", Label: "マウス", Action: "{MOUSE}", X: 0, Y: 0, W: 0.1, H: 0.16},
		{Type: "button", Label: "Esc", Action: "{ESC}", X: 0.1, Y: 0, W: 0.1, H: 0.16},
		{Type: "look", Label: "視点", X: 0.44, Y: 0, W: 0.42, H: 0.8},
		{Type: "button", Label: "左クリック", Action: "{CLICK_L}", X: 0.86, Y: 0, W: 0.14, H: 0.45},
		{Type: "button", Label: "右クリック", Action: "{CLICK_R}", X: 0.86, Y: 0.45, W: 0.14, H: 0.35},
		{Type: "button", Label: "Space", Action: "{SPACE}", X: 0.44, Y: 0.8, W: 0.28, H: 0.2},
		{Type: "scroll", Label: "ホイール", X: 0.72, Y: 0.8, W: 0.28, H: 0.2},
	}
	rows := [][]string{{"TAB", "Q", "W", "E", "R"}, {"LSHIFT", "A", "S", "D", "F"}, {"LCTRL", "Z", "X", "C", "V"}, {"1", "2", "3", "G", "T"}}
	labels := map[string]string{"TAB": "Tab", "LSHIFT": "Shift", "LCTRL": "Ctrl"}
	for r, row := range rows {
		for c, k := range row {
			l := labels[k]
			if l == "" {
				l = k
			}
			z = append(z, MouseZone{Type: "button", Label: l, Action: "{" + k + "}",
				X: 0.02 + float64(c)*0.084, Y: 0.16 + float64(r)*0.21, W: 0.084, H: 0.21})
		}
	}
	return z
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
