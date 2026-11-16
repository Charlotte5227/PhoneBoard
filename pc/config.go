package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
)

type MouseCfg struct {
	Sensitivity       float64 `json:"sensitivity"`       // 1dp あたりのピクセル数
	Accel             float64 `json:"accel"`             // 加速度 (0=なし)
	ScrollSpeed       float64 `json:"scrollSpeed"`       // スクロール倍率
	ScrollInvert      bool    `json:"scrollInvert"`      // スクロール方向反転
	SmoothScroll      bool    `json:"smoothScroll"`      // 細かい単位でスクロール
	HScroll           bool    `json:"hScroll"`           // スクロール帯で横スクロールも行う
	Momentum          bool    `json:"momentum"`          // スクロールの慣性
	TapDrag           bool    `json:"tapDrag"`           // ダブルタップ＋なぞりでドラッグ
	TapDelayMs        int     `json:"tapDelayMs"`        // ↑有効時、クリック確定までの待ち時間
	LongPress         string  `json:"longPress"`         // none / right / drag
	LongPressMs       int     `json:"longPressMs"`       // 長押し判定時間
	RightClickOneShot bool    `json:"rightClickOneShot"` // タップ=右クリック を1回で自動解除
}

type KeyboardCfg struct {
	Input            string  `json:"input"`            // gboard: スマホのキーボードをそのまま使う / flick: PhoneBoardのフリック
	Mirror           bool    `json:"mirror"`           // gboard: スマホのキーボード画面をPCに映す
	MirrorFps        int     `json:"mirrorFps"`        //
	MirrorWidth      int     `json:"mirrorWidth"`      // 送る画像の横幅(px)
	MirrorQuality    int     `json:"mirrorQuality"`    // JPEG品質
	MirrorScale      float64 `json:"mirrorScale"`      // PC画面での表示幅(倍率、キーボード表示の幅に対して)
	Style            string  `json:"style"`            // simple: なぞって離すと入力 / dwell: 止めて確定待機 / flick: 普通のフリック
	FlickSpeed       float64 `json:"flickSpeed"`       // simple: これより速い動きをフリックとみなす(dp/ms)
	Explore          bool    `json:"explore"`          // 探り操作（なぞって位置を探し、止めて確定待機）
	DwellMs          int     `json:"dwellMs"`          // 止めてから確定待機になるまで
	QuickMs          int     `json:"quickMs"`          // この時間内に離せば通常のフリックとして扱う
	FlickDp          float64 `json:"flickDp"`          // フリック判定距離(dp)
	StillDp          float64 `json:"stillDp"`          // 「止まっている」とみなす揺れ幅(dp)
	RearmDp          float64 `json:"rearmDp"`          // 確定待機後、これ以上動かすと探り直し(0=無効)
	RepeatDelayMs    int     `json:"repeatDelayMs"`    // ⌫等のリピート開始
	RepeatIntervalMs int     `json:"repeatIntervalMs"` // リピート間隔
	KanaOutput       string  `json:"kanaOutput"`       // ime: ローマ字でIMEへ送る / unicode: かなを直接送る
	ImeControl       bool    `json:"imeControl"`       // かな/英数の切替時にIMEのON/OFFを自動制御
}

type HapticCfg struct {
	Enabled  bool `json:"enabled"`
	HoverMs  int  `json:"hoverMs"`  // 別のキーに移った
	ArmMs    int  `json:"armMs"`    // 確定待機になった
	DirMs    int  `json:"dirMs"`    // フリック方向が変わった
	CommitMs int  `json:"commitMs"` // 入力した
	ToggleMs int  `json:"toggleMs"` // モード切替など
	Amp      int  `json:"amp"`      // 強さ 1-255
}

type OverlayCfg struct {
	Width       int     `json:"width"`       // キーボード表示の幅(論理px)
	Opacity     int     `json:"opacity"`     // 不透明度 %
	Anchor      string  `json:"anchor"`      // custom(自由配置) / bottom / bottom-left / bottom-right / center / top
	PosX        float64 `json:"posX"`        // custom: 画面に対する中心位置(0〜1)
	PosY        float64 `json:"posY"`        //
	OffsetX     int     `json:"offsetX"`     //
	OffsetY     int     `json:"offsetY"`     //
	ShowHints   bool    `json:"showHints"`   // キーにフリック先の文字を小さく表示
	ShowHistory bool    `json:"showHistory"` // 入力履歴を表示
	MiniMap     bool    `json:"miniMap"`     // マウスモードで触れている位置を小さく表示
	MiniWidth   int     `json:"miniWidth"`   //
	MiniAnchor  string  `json:"miniAnchor"`  //
	Toast       bool    `json:"toast"`       // モード切替などを画面に表示
}

type ButtonCfg struct {
	MouseVolUp   string `json:"mouseVolUp"`
	MouseVolDown string `json:"mouseVolDown"`
	KbVolUp      string `json:"kbVolUp"`
	KbVolDown    string `json:"kbVolDown"`
}

// configSchema は設定形式の版。上げると古い既定値を新しい既定値に置き換える。
const configSchema = 4

type Config struct {
	Schema      int         `json:"schema"`
	Port        int         `json:"port"`
	PIN         string      `json:"pin"`
	RequirePin  bool        `json:"requirePin"`
	OpenOnStart bool        `json:"openOnStart"`
	Mouse       MouseCfg    `json:"mouse"`
	Keyboard    KeyboardCfg `json:"keyboard"`
	Haptic      HapticCfg   `json:"haptic"`
	Overlay     OverlayCfg  `json:"overlay"`
	Buttons     ButtonCfg   `json:"buttons"`
}

func DefaultConfig() *Config {
	return &Config{
		Schema:      configSchema,
		Port:        39500,
		PIN:         randomPIN(),
		RequirePin:  true,
		OpenOnStart: true,
		Mouse: MouseCfg{
			Sensitivity: 2.0, Accel: 1.0, ScrollSpeed: 1.0, ScrollInvert: false,
			SmoothScroll: true, HScroll: true, Momentum: true,
			TapDrag: true, TapDelayMs: 220, LongPress: "none", LongPressMs: 550,
			RightClickOneShot: true,
		},
		Keyboard: KeyboardCfg{
			Input: "gboard", Mirror: true, MirrorFps: 15, MirrorWidth: 540, MirrorQuality: 60, MirrorScale: 1.6,
			Style: "simple", FlickSpeed: 0.25,
			Explore: true, DwellMs: 260, QuickMs: 300, FlickDp: 20, StillDp: 7, RearmDp: 90,
			RepeatDelayMs: 450, RepeatIntervalMs: 70, KanaOutput: "ime", ImeControl: true,
		},
		Haptic: HapticCfg{Enabled: true, HoverMs: 8, ArmMs: 30, DirMs: 6, CommitMs: 14, ToggleMs: 40, Amp: 200},
		Overlay: OverlayCfg{
			Width: 300, Opacity: 92, Anchor: "custom", PosX: 0.5, PosY: 0.78, OffsetX: 0, OffsetY: 0,
			ShowHints: true, ShowHistory: true, MiniMap: true, MiniWidth: 150, MiniAnchor: "bottom-right", Toast: true,
		},
		Buttons: ButtonCfg{
			MouseVolUp: "{KEYBOARD}", MouseVolDown: "{CLICK_L}",
			KbVolUp: "{MOUSE}", KbVolDown: "{MODE_NEXT}",
		},
	}
}

func (c *Config) Normalize() {
	d := DefaultConfig()
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = d.Port
	}
	if c.PIN == "" {
		c.PIN = randomPIN()
	}
	m := &c.Mouse
	if m.Sensitivity <= 0 {
		m.Sensitivity = d.Mouse.Sensitivity
	}
	if m.Accel < 0 {
		m.Accel = 0
	}
	if m.ScrollSpeed <= 0 {
		m.ScrollSpeed = d.Mouse.ScrollSpeed
	}
	if m.TapDelayMs < 50 {
		m.TapDelayMs = 50
	}
	if m.LongPress != "right" && m.LongPress != "drag" {
		m.LongPress = "none"
	}
	if m.LongPressMs < 200 {
		m.LongPressMs = 200
	}
	k := &c.Keyboard
	if k.Input != "flick" {
		k.Input = "gboard"
	}
	if k.MirrorFps < 2 || k.MirrorFps > 30 {
		k.MirrorFps = d.Keyboard.MirrorFps
	}
	if k.MirrorWidth < 240 || k.MirrorWidth > 1440 {
		k.MirrorWidth = d.Keyboard.MirrorWidth
	}
	if k.MirrorQuality < 20 || k.MirrorQuality > 95 {
		k.MirrorQuality = d.Keyboard.MirrorQuality
	}
	if k.MirrorScale < 0.5 || k.MirrorScale > 5 {
		k.MirrorScale = d.Keyboard.MirrorScale
	}
	if k.Style != "dwell" && k.Style != "flick" {
		k.Style = "simple"
	}
	k.Explore = k.Style == "dwell"
	if k.FlickSpeed < 0.05 || k.FlickSpeed > 3 {
		k.FlickSpeed = d.Keyboard.FlickSpeed
	}
	if k.DwellMs < 50 {
		k.DwellMs = 50
	}
	if k.QuickMs < k.DwellMs {
		k.QuickMs = k.DwellMs
	}
	if k.FlickDp < 5 {
		k.FlickDp = 5
	}
	if k.StillDp < 1 {
		k.StillDp = 1
	}
	if k.RepeatDelayMs < 100 {
		k.RepeatDelayMs = 100
	}
	if k.RepeatIntervalMs < 20 {
		k.RepeatIntervalMs = 20
	}
	if k.KanaOutput != "unicode" {
		k.KanaOutput = "ime"
	}
	h := &c.Haptic
	if h.Amp < 1 || h.Amp > 255 {
		h.Amp = d.Haptic.Amp
	}
	o := &c.Overlay
	if o.Width < 200 {
		o.Width = 200
	}
	if o.Opacity < 10 || o.Opacity > 100 {
		o.Opacity = d.Overlay.Opacity
	}
	if o.MiniWidth < 60 {
		o.MiniWidth = 60
	}
	if o.Anchor == "" {
		o.Anchor = "custom"
	}
	if o.PosX <= 0 || o.PosX >= 1 || o.PosY <= 0 || o.PosY >= 1 {
		o.PosX, o.PosY = d.Overlay.PosX, d.Overlay.PosY
	}
	if o.MiniAnchor == "" {
		o.MiniAnchor = "bottom-right"
	}
}

func randomPIN() string {
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return "1234"
	}
	return fmt.Sprintf("%04d", n.Int64())
}

func dataDir() string {
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("APPDATA")
	}
	if base == "" {
		if d, err := os.UserConfigDir(); err == nil {
			base = d
		} else {
			base = "."
		}
	}
	if v := os.Getenv("PHONEBOARD_DATA"); v != "" {
		base = v
	}
	dir := filepath.Join(base, "PhoneBoard")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

func loadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func saveJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadConfig() *Config {
	c := DefaultConfig()
	c.Schema = 0 // ファイルに版番号が無ければ旧版とみなす
	p := filepath.Join(dataDir(), "config.json")
	if err := loadJSON(p, c); err != nil {
		c = DefaultConfig()
	}
	if c.Schema < 2 {
		// v1 → v2: 入力方式を「かんたん」に、PC表示を小さく
		d := DefaultConfig()
		c.Keyboard.Style = d.Keyboard.Style
		c.Keyboard.FlickDp = d.Keyboard.FlickDp
		c.Keyboard.RearmDp = d.Keyboard.RearmDp
		c.Overlay.Width = d.Overlay.Width
		c.Overlay.Anchor = d.Overlay.Anchor
		c.Overlay.PosX, c.Overlay.PosY = d.Overlay.PosX, d.Overlay.PosY
		c.Overlay.OffsetX, c.Overlay.OffsetY = 0, 0
	}
	if c.Schema < 3 {
		// v2 → v3: スマホのキーボード(Gboard)入力を既定に
		c.Keyboard.Input = "gboard"
	}
	if c.Schema < 4 {
		d := DefaultConfig().Keyboard
		c.Keyboard.Mirror = true
		c.Keyboard.MirrorFps, c.Keyboard.MirrorWidth, c.Keyboard.MirrorQuality, c.Keyboard.MirrorScale = d.MirrorFps, d.MirrorWidth, d.MirrorQuality, d.MirrorScale
	}
	c.Schema = configSchema
	c.Normalize()
	_ = saveJSON(p, c)
	return c
}

func SaveConfig(c *Config) error {
	return saveJSON(filepath.Join(dataDir(), "config.json"), c)
}

func LoadLayouts() *Layouts {
	l := &Layouts{}
	p := filepath.Join(dataDir(), "layouts.json")
	if err := loadJSON(p, l); err != nil || len(l.Keyboards) == 0 || l.Schema < layoutSchema {
		if err == nil && len(l.Keyboards) > 0 {
			_ = saveJSON(filepath.Join(dataDir(), "layouts.old.json"), l) // 旧配列は退避
		}
		l = DefaultLayouts()
		_ = saveJSON(p, l)
	}
	l.Normalize()
	return l
}

func SaveLayouts(l *Layouts) error {
	return saveJSON(filepath.Join(dataDir(), "layouts.json"), l)
}
