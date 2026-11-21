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
	CommitOn         string  `json:"commitOn"`         // release: 離した時に入力（滑らせて位置を直せる） / press: 触れた瞬間に入力
	ModMode          string  `json:"modMode"`          // sticky: 1回押すと次のキーに効く（2回で固定。同時押しも可） / hold: 押している間だけ
	Assist           bool    `json:"assist"`           // 入力補助（次に来そうな文字の当たり判定を広げる）
	AssistStrength   float64 `json:"assistStrength"`   // 入力補助の強さ
	RepeatDelayMs    int     `json:"repeatDelayMs"`    // BS・矢印などのリピート開始
	RepeatIntervalMs int     `json:"repeatIntervalMs"` // リピート間隔
	ClickSound       string  `json:"clickSound"`       // pc / phone / both / off
	ClickVolume      int     `json:"clickVolume"`      // 0〜100
}

type HapticCfg struct {
	Enabled  bool `json:"enabled"`
	HoverMs  int  `json:"hoverMs"`  // 指が別のキーに移った
	CommitMs int  `json:"commitMs"` // 入力した
	ToggleMs int  `json:"toggleMs"` // モード切替・修飾キー
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
const configSchema = 5

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
			CommitOn: "release", ModMode: "sticky", Assist: true, AssistStrength: 0.8,
			RepeatDelayMs: 450, RepeatIntervalMs: 60, ClickSound: "pc", ClickVolume: 60,
		},
		Haptic: HapticCfg{Enabled: true, HoverMs: 6, CommitMs: 14, ToggleMs: 35, Amp: 200},
		Overlay: OverlayCfg{
			Width: 640, Opacity: 92, Anchor: "custom", PosX: 0.5, PosY: 0.8, OffsetX: 0, OffsetY: 0,
			ShowHints: true, ShowHistory: true, MiniMap: true, MiniWidth: 220, MiniAnchor: "bottom-right", Toast: true,
		},
		Buttons: ButtonCfg{
			MouseVolUp: "{KEYBOARD}", MouseVolDown: "{CLICK_L}",
			KbVolUp: "{MOUSE}", KbVolDown: "{KANJI}",
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
	if k.CommitOn != "press" {
		k.CommitOn = "release"
	}
	if k.ModMode != "hold" {
		k.ModMode = "sticky"
	}
	k.AssistStrength = clampF(k.AssistStrength, 0, 3)
	if k.RepeatDelayMs < 100 {
		k.RepeatDelayMs = 100
	}
	if k.RepeatIntervalMs < 20 {
		k.RepeatIntervalMs = 20
	}
	switch k.ClickSound {
	case "pc", "phone", "both", "off":
	default:
		k.ClickSound = "pc"
	}
	if k.ClickVolume < 0 || k.ClickVolume > 100 {
		k.ClickVolume = d.Keyboard.ClickVolume
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
	if c.Schema < 5 {
		// v2（横向き・JIS配列キーボード）: キーボード関連の設定を新しい既定値に
		d := DefaultConfig()
		c.Keyboard = d.Keyboard
		c.Haptic = d.Haptic
		c.Overlay.Width = d.Overlay.Width
		c.Overlay.Anchor, c.Overlay.PosX, c.Overlay.PosY = d.Overlay.Anchor, d.Overlay.PosX, d.Overlay.PosY
		c.Buttons.KbVolUp, c.Buttons.KbVolDown = d.Buttons.KbVolUp, d.Buttons.KbVolDown
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
	if err := loadJSON(p, l); err != nil || len(l.Board.Keys) == 0 || l.Schema < layoutSchema {
		if err == nil {
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
