package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type simpleRig struct {
	e   *Engine
	r   *rec
	now int64
}

func newSimple(t *testing.T) *simpleRig {
	g := &simpleRig{r: &rec{}}
	g.e = NewEngine(func(ev EngineEvent) {
		if ev.Kind == "commit" {
			g.r.commits = append(g.r.commits, ev.Text)
		}
	})
	g.e.Clock = func() int64 { return g.now }
	cfg := DefaultConfig().Keyboard
	l := DefaultLayouts()
	g.e.Configure(cfg, &l.Keyboards[0], l.Region, 412, 915)
	return g
}

func (g *simpleRig) at(label string) (float64, float64) { return keyCenter(g.e, label) }
func (g *simpleRig) dp(dx, dy float64) (float64, float64) {
	return dx / g.e.wdp, dy / g.e.hdp
}

// ゆっくり移動（0.08dp/ms 程度）
func (g *simpleRig) slide(x0, y0, x1, y1 float64, ms int64) {
	n := ms / 16
	for i := int64(1); i <= n; i++ {
		f := float64(i) / float64(n)
		g.now += 16
		g.e.Move(x0+(x1-x0)*f, y0+(y1-y0)*f, g.now)
		g.e.Tick(g.now)
	}
}

func (g *simpleRig) wait(ms int64) {
	for i := int64(0); i < ms; i += 10 {
		g.now += 10
		g.e.Tick(g.now)
	}
}

func (g *simpleRig) got() string { return strings.Join(g.r.commits, ",") }

func TestSimpleTapAndQuickFlick(t *testing.T) {
	g := newSimple(t)
	x, y := g.at("か")
	g.e.Down(x, y, g.now)
	g.now += 70
	g.e.Up(x, y, g.now)
	dx, _ := g.dp(-30, 0)
	g.now += 500
	g.e.Down(x, y, g.now)
	g.now += 16
	g.e.Move(x+dx/2, y, g.now)
	g.now += 16
	g.e.Up(x+dx, y, g.now)
	if g.got() != "か,き" {
		t.Fatal(g.got())
	}
}

func TestSimpleSlideThenLift(t *testing.T) {
	g := newSimple(t)
	ax, ay := g.at("あ")
	hx, hy := g.at("は")
	g.e.Down(ax, ay, g.now)
	g.slide(ax, ay, hx, hy, 1200)
	if s := g.e.Snap(); s.Preview != "は" || s.Armed {
		t.Fatalf("preview=%q armed=%v", s.Preview, s.Armed)
	}
	g.now += 16
	g.e.Up(hx, hy, g.now)
	if g.got() != "は" {
		t.Fatal(g.got())
	}
}

func TestSimpleSlideThenFlick(t *testing.T) {
	g := newSimple(t)
	ax, ay := g.at("あ")
	hx, hy := g.at("は")
	g.e.Down(ax, ay, g.now)
	g.slide(ax, ay, hx, hy, 1200)
	g.wait(300) // 少し止めて確認しても良い（リピートキーではないので何も起きない）
	dx, _ := g.dp(14, 0)
	g.now += 16
	g.e.Move(hx+dx, hy, g.now)
	g.now += 16
	g.e.Move(hx+2*dx, hy, g.now)
	if s := g.e.Snap(); !s.Armed || s.Preview != "へ" {
		t.Fatalf("snap %+v", s)
	}
	g.now += 30
	g.e.Up(hx+2*dx, hy, g.now)
	if g.got() != "へ" {
		t.Fatal(g.got())
	}
}

func TestSimpleCancelOutside(t *testing.T) {
	g := newSimple(t)
	ax, ay := g.at("あ")
	g.e.Down(ax, ay, g.now)
	g.slide(ax, ay, ax, 0.3, 1500) // キーボードの上へ外す
	g.now += 16
	g.e.Up(ax, 0.3, g.now)
	if g.got() != "" {
		t.Fatal(g.got())
	}
	if !g.e.Snap().Canceled {
		t.Fatal("not canceled")
	}
}

func TestSimpleFastMoveThenStop(t *testing.T) {
	g := newSimple(t)
	ax, ay := g.at("あ")
	rx, ry := g.at("ら")
	g.e.Down(ax, ay, g.now)
	// 素早く ら まで移動して止める → フリック扱いを解除して ら
	for i := 1; i <= 4; i++ {
		f := float64(i) / 4
		g.now += 16
		g.e.Move(ax+(rx-ax)*f, ay+(ry-ay)*f, g.now)
	}
	g.wait(500)
	g.now += 16
	g.e.Up(rx, ry, g.now)
	if g.got() != "ら" {
		t.Fatal(g.got())
	}
}

func TestSimpleRepeatOnlyWhenTouchedDirectly(t *testing.T) {
	g := newSimple(t)
	bx, by := g.at("削除")
	g.e.Down(bx, by, g.now)
	g.wait(1200)
	g.e.Up(bx, by, g.now)
	n := len(g.r.commits)
	if n < 5 {
		t.Fatalf("repeat %d", n)
	}
	// なぞって来て止めた場合はリピートしない（離すと1回）
	g.r.commits = nil
	sx, sy := g.at("さ")
	g.e.Down(sx, sy, g.now)
	g.slide(sx, sy, bx, by, 600)
	g.wait(1500)
	g.e.Up(bx, by, g.now)
	if g.got() != "{BS}" {
		t.Fatal(g.got())
	}
}

func TestConfigMigration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PHONEBOARD_DATA", dir)
	old := DefaultConfig()
	old.Schema = 0
	old.Overlay.Width = 520
	old.Overlay.Anchor = "bottom"
	old.Keyboard.Style = ""
	old.PIN = "4321"
	b, _ := json.Marshal(old)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	delete(m, "schema")
	b, _ = json.Marshal(m)
	_ = os.WriteFile(filepath.Join(dataDir(), "config.json"), b, 0o644)
	c := LoadConfig()
	if c.Overlay.Width != 300 || c.Overlay.Anchor != "custom" || c.Keyboard.Style != "simple" || c.PIN != "4321" || c.Schema != configSchema {
		t.Fatalf("%+v %+v %s", c.Overlay, c.Keyboard, c.PIN)
	}
	c.Overlay.Width = 444
	_ = SaveConfig(c)
	if LoadConfig().Overlay.Width != 444 {
		t.Fatal("user value overwritten")
	}
}
