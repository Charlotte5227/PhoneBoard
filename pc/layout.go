package main

import (
	"encoding/json"
	"strings"
)

// Key はフリックキー1つ分。X,Y,W,H はキーボード内のグリッド単位。
// C/L/U/R/D は 中央(タップ)/左/上/右/下 フリックで出力する文字列。
// "{ENTER}" のように {} で囲むとアクション（特殊キー）になる。
type Key struct {
	Label  string  `json:"label"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h"`
	C      string  `json:"c"`
	L      string  `json:"l"`
	U      string  `json:"u"`
	R      string  `json:"r"`
	D      string  `json:"d"`
	Repeat bool    `json:"repeat,omitempty"`
}

// Slot は方向に応じた出力を返す。
func (k *Key) Slot(d Dir) string {
	switch d {
	case DirL:
		return k.L
	case DirU:
		return k.U
	case DirR:
		return k.R
	case DirD:
		return k.D
	}
	return k.C
}

// KbLayout は1種類のキー配列（かな / 英字 / 数字 など）。
type KbLayout struct {
	Name  string `json:"name"`  // 内部名 (kana, alpha, num, ...)
	Title string `json:"title"` // 表示名
	Kind  string `json:"kind"`  // kana: IMEローマ字入力 / alpha・num: 直接入力
	Cols  int    `json:"cols"`
	Rows  int    `json:"rows"`
	Keys  []Key  `json:"keys"`
}

// Region はスマホ画面上のキーボード領域（画面に対する割合 0〜1）。
type Region struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
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

type Layouts struct {
	Region     Region      `json:"region"`
	Keyboards  []KbLayout  `json:"keyboards"`
	MouseZones []MouseZone `json:"mouseZones"`
}

func (ls *Layouts) Find(name string) (int, *KbLayout) {
	for i := range ls.Keyboards {
		if ls.Keyboards[i].Name == name {
			return i, &ls.Keyboards[i]
		}
	}
	return -1, nil
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
	r := &ls.Region
	r.X = clamp(r.X, 0, 0.95)
	r.Y = clamp(r.Y, 0, 0.95)
	r.W = clamp(r.W, 0.05, 1-r.X)
	r.H = clamp(r.H, 0.05, 1-r.Y)
	if len(ls.Keyboards) == 0 {
		ls.Keyboards = DefaultLayouts().Keyboards
	}
	for i := range ls.Keyboards {
		kb := &ls.Keyboards[i]
		if kb.Cols < 1 {
			kb.Cols = 1
		}
		if kb.Rows < 1 {
			kb.Rows = 1
		}
		if kb.Name == "" {
			kb.Name = "layout" + itoa(i)
		}
		if kb.Title == "" {
			kb.Title = kb.Name
		}
		if kb.Kind != "kana" {
			kb.Kind = strings.ToLower(kb.Kind)
			if kb.Kind != "alpha" && kb.Kind != "num" {
				kb.Kind = "alpha"
			}
		}
		for j := range kb.Keys {
			k := &kb.Keys[j]
			if k.W <= 0 {
				k.W = 1
			}
			if k.H <= 0 {
				k.H = 1
			}
		}
	}
	for i := range ls.MouseZones {
		z := &ls.MouseZones[i]
		z.X = clamp(z.X, 0, 1)
		z.Y = clamp(z.Y, 0, 1)
		z.W = clamp(z.W, 0.01, 1)
		z.H = clamp(z.H, 0.01, 1)
		if z.Type != "pad" && z.Type != "scroll" && z.Type != "button" {
			z.Type = "pad"
		}
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

func k5(label string, x, y float64, c, l, u, r, d string) Key {
	return Key{Label: label, X: x, Y: y, W: 1, H: 1, C: c, L: l, U: u, R: r, D: d}
}

// 片手（親指）操作を想定した既定の配列。5列×4行のiPhone風配置。
// 左列: カーソル/モード、中央3列: 文字、右列: 削除/変換/改行。
func sideKeys(modeLabel string) []Key {
	bs := k5("削除", 4, 0, "{BS}", "{CTRL+BS}", "{ESC}", "{DEL}", "")
	bs.Repeat = true
	left := k5("←", 0, 0, "{LEFT}", "{HOME}", "{UP}", "{RIGHT}", "{DOWN}")
	left.Repeat = true
	right := k5("→", 0, 1, "{RIGHT}", "{LEFT}", "{UP}", "{END}", "{DOWN}")
	right.Repeat = true
	enter := Key{Label: "改行", X: 4, Y: 2, W: 1, H: 2, C: "{ENTER}", U: "{SHIFT+ENTER}", L: "{TAB}", R: "{CTRL+ENTER}", D: "{ESC}"}
	return []Key{
		left, right,
		k5(modeLabel, 0, 2, "{MODE_NEXT}", "{MODE:kana}", "{MODE:alpha}", "{MODE:num}", "{MOUSE}"),
		k5("編集", 0, 3, "{CTRL+V}", "{CTRL+X}", "{CTRL+C}", "{CTRL+A}", "{CTRL+Z}"),
		bs,
		k5("空白", 4, 1, "{SPACE}", "{F6}", "{F7}", "{F10}", "{TAB}"),
		enter,
	}
}

func DefaultLayouts() *Layouts {
	kana := KbLayout{Name: "kana", Title: "かな", Kind: "kana", Cols: 5, Rows: 4}
	kana.Keys = append(sideKeys("ABC\n123"),
		k5("あ", 1, 0, "あ", "い", "う", "え", "お"),
		k5("か", 2, 0, "か", "き", "く", "け", "こ"),
		k5("さ", 3, 0, "さ", "し", "す", "せ", "そ"),
		k5("た", 1, 1, "た", "ち", "つ", "て", "と"),
		k5("な", 2, 1, "な", "に", "ぬ", "ね", "の"),
		k5("は", 3, 1, "は", "ひ", "ふ", "へ", "ほ"),
		k5("ま", 1, 2, "ま", "み", "む", "め", "も"),
		k5("や", 2, 2, "や", "「", "ゆ", "」", "よ"),
		k5("ら", 3, 2, "ら", "り", "る", "れ", "ろ"),
		k5("゛゜小", 1, 3, "{CYCLE}", "", "{DAKU}", "{HANDAKU}", "{SMALL}"),
		k5("わ", 2, 3, "わ", "を", "ん", "ー", "〜"),
		k5("、。?!", 3, 3, "、", "。", "？", "！", "…"),
	)

	alpha := KbLayout{Name: "alpha", Title: "ABC", Kind: "alpha", Cols: 5, Rows: 4}
	alpha.Keys = append(sideKeys("123\nかな"),
		k5("@#/&_", 1, 0, "@", "#", "/", "&", "_"),
		k5("ABC", 2, 0, "a", "b", "c", "", ""),
		k5("DEF", 3, 0, "d", "e", "f", "", ""),
		k5("GHI", 1, 1, "g", "h", "i", "", ""),
		k5("JKL", 2, 1, "j", "k", "l", "", ""),
		k5("MNO", 3, 1, "m", "n", "o", "", ""),
		k5("PQRS", 1, 2, "p", "q", "r", "s", ""),
		k5("TUV", 2, 2, "t", "u", "v", "", ""),
		k5("WXYZ", 3, 2, "w", "x", "y", "z", ""),
		k5("a/A", 1, 3, "{CASE}", "", "{CAPS}", "", ""),
		k5("'\"()", 2, 3, "'", "\"", "(", ")", ""),
		k5(".,?!", 3, 3, ".", ",", "?", "!", "-"),
	)

	num := KbLayout{Name: "num", Title: "123", Kind: "num", Cols: 5, Rows: 4}
	num.Keys = append(sideKeys("かな\nABC"),
		k5("1", 1, 0, "1", "!", "@", "#", ""),
		k5("2", 2, 0, "2", "$", "%", "^", ""),
		k5("3", 3, 0, "3", "&", "*", "~", ""),
		k5("4", 1, 1, "4", "+", "-", "=", ""),
		k5("5", 2, 1, "5", "<", ">", "|", ""),
		k5("6", 3, 1, "6", "(", ")", "\\", ""),
		k5("7", 1, 2, "7", "[", "]", ":", ""),
		k5("8", 2, 2, "8", "{", "}", ";", ""),
		k5("9", 3, 2, "9", "\"", "'", "`", ""),
		k5("()", 1, 3, "(", ")", "[", "]", ""),
		k5("0", 2, 3, "0", "_", "~", "?", ""),
		k5(".,-/", 3, 3, ".", ",", "-", "/", ""),
	)

	return &Layouts{
		// 右手親指が届きやすい画面下側に配置
		Region:    Region{X: 0, Y: 0.42, W: 1, H: 0.58},
		Keyboards: []KbLayout{kana, alpha, num},
		MouseZones: []MouseZone{
			{Type: "pad", Label: "パッド", X: 0, Y: 0, W: 0.8, H: 0.82},
			{Type: "scroll", Label: "スクロール", X: 0.8, Y: 0, W: 0.2, H: 0.82},
			{Type: "button", Label: "キーボード", Action: "{KEYBOARD}", X: 0, Y: 0.82, W: 0.3, H: 0.18},
			{Type: "button", Label: "左右切替", Action: "{TAPMODE}", X: 0.3, Y: 0.82, W: 0.3, H: 0.18},
			{Type: "button", Label: "右クリック", Action: "{CLICK_R}", X: 0.6, Y: 0.82, W: 0.4, H: 0.18},
		},
	}
}
