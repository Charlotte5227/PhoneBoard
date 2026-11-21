package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type kout struct {
	log   []string
	warns []string
}

func (o *kout) KeyPress(ev KeyEvent) {
	s := ""
	for _, m := range ev.Mods {
		s += map[uint16]string{0xA0: "S+", 0xA2: "C+", 0xA4: "A+", 0x5B: "W+"}[m]
	}
	o.log = append(o.log, s+ev.Label)
	if ev.Warn != "" {
		o.warns = append(o.warns, ev.Label+":"+ev.Warn)
	}
}
func (o *kout) ModKey(vk uint16, down bool) {
	o.log = append(o.log, fmt.Sprintf("mod%#x:%v", vk, down))
}
func (o *kout) Action(a string)      { o.log = append(o.log, a) }
func (o *kout) Feedback(kind string) {}

type krig struct {
	k   *Kbd
	o   *kout
	b   *Board
	t   int64
	ime bool
}

func newKrig(mut func(c *KeyboardCfg)) *krig {
	r := &krig{o: &kout{}}
	r.k = NewKbd(r.o)
	r.k.IME = func() bool { return r.ime }
	cfg := DefaultConfig().Keyboard
	if mut != nil {
		mut(&cfg)
	}
	l := DefaultLayouts()
	r.b = &l.Board
	r.k.Configure(cfg, r.b)
	return r
}

// at はキーの中心から (dx,dy)キー単位ずらした画面座標。
func (r *krig) at(label string, dx, dy float64) (float64, float64) {
	for _, k := range r.b.Keys {
		if k.Label == label {
			return (k.X + k.W/2 + dx*k.W) / r.b.Cols, (k.Y + k.H/2 + dy*k.H) / r.b.Rows
		}
	}
	panic("no key " + label)
}

func (r *krig) tap(id int, label string, dx, dy float64) {
	x, y := r.at(label, dx, dy)
	r.t += 50
	r.k.Down(id, x, y, r.t)
	r.t += 60
	r.k.Up(id, x, y, r.t)
}

func (r *krig) got() string { s := strings.Join(r.o.log, ","); r.o.log = nil; return s }

func TestTapReleaseAndSlide(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false })
	r.tap(1, "Q", 0, 0)
	// Q に触れて W まで滑らせて離す → W
	x0, y0 := r.at("Q", 0, 0)
	x1, y1 := r.at("W", 0, 0)
	r.k.Down(1, x0, y0, 1000)
	r.k.Move(1, (x0+x1)/2, y0, 1050)
	r.k.Move(1, x1, y1, 1100)
	r.k.Up(1, x1, y1, 1150)
	// 最上段の空き（キーの無い所）へ逃がして離す → 取り消し
	r.k.Down(2, x0, y0, 2000)
	r.k.Up(2, 0.5, 0.02, 2100)
	if g := r.got(); g != "q,w" {
		t.Fatal(g)
	}
}

func TestPressMode(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false; c.CommitOn = "press" })
	x, y := r.at("A", 0, 0)
	r.k.Down(1, x, y, 0)
	if g := r.got(); g != "a" {
		t.Fatal(g)
	}
	x2, y2 := r.at("S", 0, 0)
	r.k.Move(1, x2, y2, 50)
	r.k.Up(1, x2, y2, 100)
	if g := r.got(); g != "" {
		t.Fatal("should not type again: " + g)
	}
}

func TestStickyShift(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false })
	r.tap(1, "Shift", 0, 0)
	r.tap(1, "A", 0, 0)
	r.tap(1, "B", 0, 0)
	if g := r.got(); g != "S+A,b" {
		t.Fatal(g)
	}
	// 2回で固定、3回目で解除
	r.tap(1, "Ctrl", 0, 0)
	r.tap(1, "Ctrl", 0, 0)
	r.tap(1, "C", 0, 0)
	r.tap(1, "V", 0, 0)
	r.tap(1, "Ctrl", 0, 0)
	r.tap(1, "X", 0, 0)
	if g := r.got(); g != "C+c,C+v,x" {
		t.Fatal(g)
	}
	// 1をShift付き → ラベルは !
	r.tap(1, "Shift", 0, 0)
	r.tap(1, "1", 0, 0)
	if g := r.got(); g != "S+!" {
		t.Fatal(g)
	}
}

func TestChordInStickyMode(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false })
	// 左手でCtrlを押さえたまま右手でZ → Ctrl+Z。Ctrlはワンショットにならない
	cx, cy := r.at("Ctrl", 0, 0)
	r.k.Down(1, cx, cy, 0)
	r.tap(2, "Z", 0, 0)
	r.k.Up(1, cx, cy, 300)
	r.tap(2, "Z", 0, 0)
	if g := r.got(); g != "C+z,z" {
		t.Fatal(g)
	}
}

func TestHoldMode(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false; c.ModMode = "hold" })
	sx, sy := r.at("Shift", 0, 0)
	r.k.Down(1, sx, sy, 0)
	r.tap(2, "A", 0, 0)
	r.k.Up(1, sx, sy, 300)
	r.tap(2, "Shift", 0, 0) // 同時押し方式では単独タップしても次に効かない
	r.tap(2, "B", 0, 0)
	if g := r.got(); g != "mod0xa0:true,A,mod0xa0:false,mod0xa0:true,mod0xa0:false,b" {
		t.Fatal(g)
	}
}

func TestFnLayer(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false })
	r.tap(1, "Fn", 0, 0)
	r.tap(1, "1", 0, 0)
	r.tap(1, "1", 0, 0)
	r.tap(1, "Fn", 0, 0)
	r.tap(1, "Fn", 0, 0) // 固定
	r.tap(1, "←", 0, 0)
	r.tap(1, "BS", 0, 0)
	r.tap(1, "Fn", 0, 0) // 解除
	r.tap(1, "マウス", 0, 0)
	if g := r.got(); g != "F1,1,Home,Del,{MOUSE}" {
		t.Fatal(g)
	}
}

func TestRepeat(t *testing.T) {
	r := newKrig(nil)
	x, y := r.at("BS", 0, 0)
	r.k.Down(1, x, y, 0)
	for tt := int64(0); tt <= 1000; tt += 10 {
		r.k.Tick(tt)
	}
	r.k.Up(1, x, y, 1000)
	n := len(r.o.log)
	if n < 8 {
		t.Fatalf("repeat %d", n)
	}
	r.got()
	// なぞってBSに来た場合はリピートしない
	ox, oy := r.at("0", 0, 0)
	r.k.Down(1, ox, oy, 2000)
	r.k.Move(1, x, y, 2050)
	for tt := int64(2050); tt <= 3500; tt += 10 {
		r.k.Tick(tt)
	}
	r.k.Up(1, x, y, 3500)
	if g := r.got(); g != "BS" {
		t.Fatal(g)
	}
}

func TestAssistRomaji(t *testing.T) {
	r := newKrig(nil)
	r.ime = true
	r.tap(1, "K", 0, 0)
	// A と S の境目より少しS寄りを押す → ローマ字としては母音の A が自然
	r.tap(1, "A", 0.6, 0)
	if g := r.got(); g != "k,a" {
		t.Fatal(g)
	}
	// 補助なしなら S
	r2 := newKrig(func(c *KeyboardCfg) { c.Assist = false })
	r2.tap(1, "K", 0, 0)
	r2.tap(1, "A", 0.6, 0)
	if g := r2.got(); g != "k,s" {
		t.Fatal(g)
	}
	// キーの中心付近をしっかり押した時は補助しない（kの次でもSはS）
	r.tap(1, "K", 0, 0)
	r.tap(1, "S", 0, 0)
	if g := r.got(); g != "k,s" {
		t.Fatal(g)
	}
}

func TestAssistEnglish(t *testing.T) {
	r := newKrig(nil)
	// "th" の次: E と R の境目（R寄り）→ 英語として自然な e
	r.tap(1, "T", 0, 0)
	r.tap(1, "H", 0, 0)
	r.tap(1, "E", 0.58, 0)
	if g := r.got(); g != "t,h,e" {
		t.Fatal(g)
	}
	// "q" の次: U と Y の境目（Y寄り）→ u
	r.tap(1, "Space", 0, 0)
	r.tap(1, "Q", 0, 0)
	r.tap(1, "U", -0.58, 0)
	if g := r.got(); g != "Space,q,u" {
		t.Fatal(g)
	}
}

func TestPrior(t *testing.T) {
	if romajiPending("kyo") != "" || romajiPending("sh") != "sh" || romajiPending("kak") != "k" || romajiPending("ann") != "" || romajiPending("nk") != "k" {
		t.Fatal("pending")
	}
	if !(Prior("k", 'a', true) > 10*Prior("k", 's', true)) {
		t.Fatal("romaji k")
	}
	if !(Prior("th", 'e', false) > Prior("th", 'r', false)*3) {
		t.Fatal("english th")
	}
	if Prior("xqzv", 'a', false) != 1 {
		t.Fatal("unknown word should be neutral")
	}
}

func TestLayoutNoOverlap(t *testing.T) {
	b := DefaultLayouts().Board
	for i, a := range b.Keys {
		if a.X < 0 || a.Y < 0 || a.X+a.W > b.Cols+1e-9 || a.Y+a.H > b.Rows+1e-9 {
			t.Fatalf("%s out of board", a.Label)
		}
		for j := i + 1; j < len(b.Keys); j++ {
			c := b.Keys[j]
			if a.X < c.X+c.W-1e-9 && c.X < a.X+a.W-1e-9 && a.Y < c.Y+c.H-1e-9 && c.Y < a.Y+a.H-1e-9 {
				t.Fatalf("overlap %s / %s", a.Label, c.Label)
			}
		}
		for _, n := range []string{a.Key, a.FnKey} {
			if n == "" {
				continue
			}
			if _, _, ok := ParseCombo(n); !ok {
				t.Fatalf("bad key name %q", n)
			}
		}
	}
}

func TestConfigMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PHONEBOARD_DATA", dir)
	old := map[string]any{"schema": 4, "pin": "4321", "port": 39500,
		"keyboard": map[string]any{"input": "gboard", "mirror": true},
		"overlay":  map[string]any{"width": 300, "anchor": "custom", "posX": 0.3, "posY": 0.3}}
	b, _ := json.Marshal(old)
	_ = os.WriteFile(filepath.Join(dataDir(), "config.json"), b, 0o644)
	_ = os.WriteFile(filepath.Join(dataDir(), "layouts.json"), []byte(`{"schema":2,"keyboards":[{"name":"kana"}]}`), 0o644)
	c := LoadConfig()
	if c.PIN != "4321" || c.Keyboard.CommitOn != "release" || c.Keyboard.ModMode != "sticky" || c.Overlay.Width != 640 || c.Schema != configSchema {
		t.Fatalf("%+v %+v", c.Keyboard, c.Overlay)
	}
	l := LoadLayouts()
	if len(l.Board.Keys) < 60 || l.Schema != layoutSchema {
		t.Fatal("layout not migrated")
	}
	if _, err := os.Stat(filepath.Join(dataDir(), "layouts.old.json")); err != nil {
		t.Fatal("old layout not backed up")
	}
}

func TestSlipGuard(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false; c.AutoCalib = false })
	// G を押して、離す直前(20ms)に指が転がって H に乗った → G
	x0, y0 := r.at("G", 0.3, 0)
	x1, y1 := r.at("H", -0.4, 0)
	r.k.Down(1, x0, y0, 1000)
	r.k.Move(1, x1, y1, 1100)
	r.k.Up(1, x1, y1, 1120)
	// 滑らせて H に移り、少し置いてから離した → H
	r.k.Down(1, x0, y0, 2000)
	r.k.Move(1, x1, y1, 2100)
	r.k.Up(1, x1, y1, 2200)
	if g := r.got(); g != "g,h" {
		t.Fatal(g)
	}
}

func TestAutoCalib(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false })
	// 右手側でいつもキーの中心より 0.4 下を押すクセ
	for i := 0; i < 60; i++ {
		r.tap(1, "U", 0, 0.4)
		r.tap(1, "I", 0, 0.4)
	}
	r.got()
	if r.k.Calib.Y[1] < 0.2 || math.Abs(r.k.Calib.Y[0]) > 0.01 {
		t.Fatalf("calib %+v", r.k.Calib)
	}
	// 補正後: U の中心から 0.55 下（本来は J の領域）を押しても U
	r.tap(1, "U", 0, 0.55)
	if g := r.got(); g != "u" {
		t.Fatal(g)
	}
	// BS で消したキーは学習しない
	before := r.k.Calib
	r.tap(1, "O", 0.45, -0.45)
	r.tap(1, "BS", 0, 0)
	r.tap(1, "O", 0, 0)
	if r.k.Calib.X[1] != before.X[1] && math.Abs(r.k.Calib.X[1]-before.X[1]) > 0.001 {
		t.Fatalf("learned from typo: %+v -> %+v", before, r.k.Calib)
	}
}

func (r *krig) typeWord(w string) {
	for _, c := range strings.ToUpper(w) {
		r.tap(1, string(c), 0, 0)
	}
}

func TestWarnRomaji(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false; c.AutoCalib = false })
	r.ime = true
	for _, w := range []string{"kakko", "konnnichiha", "kyouha", "shita", "tsukue", "xtu", "nkai", "chotto", "dhi", "fyu"} {
		r.typeWord(w)
		r.tap(1, "Space", 0, 0)
		if len(r.o.warns) != 0 {
			t.Fatalf("%s: unexpected warn %v", w, r.o.warns)
		}
	}
	r.typeWord("kt")
	if strings.Join(r.o.warns, ",") != "t:romaji" {
		t.Fatalf("kt: %v", r.o.warns)
	}
	r.o.warns = nil
	r.tap(1, "Space", 0, 0)
	r.typeWord("asdf")
	if strings.Join(r.o.warns, ",") != "d:romaji,f:romaji" {
		t.Fatalf("asdf: %v", r.o.warns)
	}
}

func TestWarnAlphaJapanese(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false; c.AutoCalib = false })
	r.ime = false
	// 普通の英語では鳴らない
	for _, w := range []string{"banana", "because", "tomato", "karaoke", "information", "hello"} {
		r.typeWord(w)
		r.tap(1, "Space", 0, 0)
	}
	if len(r.o.warns) != 0 {
		t.Fatalf("english warned: %v", r.o.warns)
	}
	// 英字のまま日本語 → 早い段階で一度だけ
	r.typeWord("konnnitiwa")
	r.tap(1, ",", 0, 0)
	r.typeWord("watasihaosusigatabetaidesu")
	if strings.Join(r.o.warns, ",") != "i:alpha" {
		t.Fatalf("warns: %v", r.o.warns)
	}
	// IMEをON→OFFにした・アプリを切り替えた後は、また鳴る
	r.o.warns = nil
	r.k.ResetJaWarn()
	r.tap(1, "Space", 0, 0)
	r.typeWord("watasiha")
	if strings.Join(r.o.warns, ",") != "i:alpha" {
		t.Fatalf("after reset: %v", r.o.warns)
	}
}

func TestSoundMixer(t *testing.T) {
	m := NewMixer()
	for _, g := range SoundGroups {
		if s := synth(g); len(s) < 500 {
			t.Fatalf("%s too short", g)
		}
	}
	// 速く打った音が重なっても途切れず、最後まで鳴って消える
	m.Add(SndLetter, 0, 80)
	buf := make([]int16, 200)
	m.Fill(buf)
	m.Add(SndVowel, 12, 80)
	m.Add(SndHeavy, 6, 80)
	nonzero := 0
	for i := 0; i < 30; i++ {
		m.Fill(buf)
		for _, v := range buf {
			if v != 0 {
				nonzero++
			}
		}
	}
	if nonzero == 0 || m.Active() {
		t.Fatalf("mixer: nonzero=%d active=%v", nonzero, m.Active())
	}
	if w := renderWAV(SndWarn, 0, 70); len(w) < 1000 || string(w[:4]) != "RIFF" {
		t.Fatal("wav")
	}
}

func TestWarnKana(t *testing.T) {
	r := newKrig(func(c *KeyboardCfg) { c.Assist = false; c.AutoCalib = false })
	r.ime = true
	for _, w := range []string{"watashihasushigatabetaidesu", "kyouhaiitenkidesune", "arigatougozaimasu", "otsukaresamadeshita", "konnnichiha"} {
		r.typeWord(w)
		r.tap(1, "Space", 0, 0)
	}
	if len(r.o.warns) != 0 {
		t.Fatalf("normal warned: %v", r.o.warns)
	}
	// 明らかに変な並び（たゔぇ）→ 1回だけ
	r.typeWord("tavetaitavetai")
	if strings.Join(r.o.warns, ",") != "e:kana" {
		t.Fatalf("warns: %v", r.o.warns)
	}
	// 区切り（Space）の後はまた鳴る
	r.o.warns = nil
	r.tap(1, "Space", 0, 0)
	r.typeWord("kyuouha")
	if strings.Join(r.o.warns, ",") != "o:kana" {
		t.Fatalf("after space: %v", r.o.warns)
	}
}

func TestRomaToMora(t *testing.T) {
	for in, want := range map[string]string{
		"konnnichiha": "こ,ん,に,ち,は", "kitte": "き,っ,て", "shinkansen": "し,ん,か,ん,せ", "kyouha": "きょ,う,は",
		"tsukue": "つ,く,え", "ge-mu": "げ,ー,む", "fairu": "ふぁ,い,る", "xtu": "っ", "dhi": "でぃ",
	} {
		m, p := romaToMora(in)
		if got := strings.Join(m, ","); got != want {
			t.Errorf("%s: %s (pending %q)", in, got, p)
		}
	}
}
