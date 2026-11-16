package main

import (
	"strings"
	"testing"
)

type rec struct {
	commits []string
	haptics []string
}

func newTestEngine(explore bool) (*Engine, *rec) {
	r := &rec{}
	e := NewEngine(func(ev EngineEvent) {
		if ev.Kind == "commit" {
			r.commits = append(r.commits, ev.Text)
		} else {
			r.haptics = append(r.haptics, ev.Haptic)
		}
	})
	cfg := DefaultConfig().Keyboard
	cfg.Style = "flick"
	if explore {
		cfg.Style = "dwell"
	}
	cfg.Explore = explore
	cfg.FlickDp = 26
	cfg.RearmDp = 110
	l := DefaultLayouts()
	e.Configure(cfg, &l.Keyboards[0], l.Region, 400, 860)
	e.Clock = nil
	return e, r
}

// キー中心の画面座標 (0〜1)
func keyCenter(e *Engine, label string) (float64, float64) {
	for _, k := range e.lay.Keys {
		if k.Label == label {
			gx := (k.X + k.W/2) / float64(e.lay.Cols)
			gy := (k.Y + k.H/2) / float64(e.lay.Rows)
			return e.reg.X + gx*e.reg.W, e.reg.Y + gy*e.reg.H
		}
	}
	panic("no key " + label)
}

func dp(e *Engine, dx, dy float64) (float64, float64) { return dx / e.wdp, dy / e.hdp }

func TestQuickTapAndFlick(t *testing.T) {
	e, r := newTestEngine(true)
	x, y := keyCenter(e, "か")
	e.Down(x, y, 0)
	e.Up(x, y, 80)
	ox, oy := dp(e, -40, 0)
	e.Down(x, y, 1000)
	e.Move(x+ox/2, y, 1030)
	e.Up(x+ox, y+oy, 1060)
	ux, uy := dp(e, 0, -40)
	e.Down(x, y, 2000)
	e.Up(x+ux, y+uy, 2090)
	if strings.Join(r.commits, ",") != "か,き,く" {
		t.Fatalf("got %v", r.commits)
	}
}

func TestExploreDwellFlick(t *testing.T) {
	e, r := newTestEngine(true)
	ax, ay := keyCenter(e, "あ")
	hx, hy := keyCenter(e, "は")
	e.Down(ax, ay, 0)
	// なぞって は まで移動（時間をかける）
	for i := 1; i <= 20; i++ {
		f := float64(i) / 20
		e.Move(ax+(hx-ax)*f, ay+(hy-ay)*f, int64(i*30))
		e.Tick(int64(i * 30))
	}
	if e.state != stExplore {
		t.Fatalf("should still explore, state=%v", e.state)
	}
	if e.lay.Keys[e.hover].Label != "は" {
		t.Fatalf("hover=%s", e.lay.Keys[e.hover].Label)
	}
	// 止める → 確定待機
	for tt := int64(600); tt <= 900; tt += 10 {
		e.Tick(tt)
	}
	if e.state != stArmed {
		t.Fatal("not armed")
	}
	dx, _ := dp(e, 35, 0)
	e.Move(hx+dx, hy, 950)
	e.Up(hx+dx, hy, 1000)
	if strings.Join(r.commits, ",") != "へ" {
		t.Fatalf("got %v", r.commits)
	}
}

func TestExploreCancel(t *testing.T) {
	e, r := newTestEngine(true)
	ax, ay := keyCenter(e, "あ")
	hx, hy := keyCenter(e, "は")
	e.Down(ax, ay, 0)
	for i := 1; i <= 20; i++ {
		f := float64(i) / 20
		e.Move(ax+(hx-ax)*f, ay+(hy-ay)*f, int64(i*30))
		e.Tick(int64(i * 30))
	}
	e.Up(hx, hy, 610)
	if len(r.commits) != 0 {
		t.Fatalf("should cancel, got %v", r.commits)
	}
}

func TestRepeatBackspace(t *testing.T) {
	e, r := newTestEngine(true)
	x, y := keyCenter(e, "削除")
	e.Down(x, y, 0)
	for tt := int64(0); tt <= 1500; tt += 10 {
		e.Tick(tt)
	}
	e.Up(x, y, 1500)
	if len(r.commits) < 5 {
		t.Fatalf("expected repeats, got %v", r.commits)
	}
	for _, c := range r.commits {
		if c != "{BS}" {
			t.Fatalf("bad %v", r.commits)
		}
	}
}

func TestRearm(t *testing.T) {
	e, r := newTestEngine(true)
	ax, ay := keyCenter(e, "あ")
	e.Down(ax, ay, 0)
	for tt := int64(0); tt <= 400; tt += 10 {
		e.Tick(tt)
	}
	if e.state != stArmed {
		t.Fatal("not armed")
	}
	// 大きく動かす → 探り直し
	rx, ry := keyCenter(e, "ら")
	e.Move(rx, ry, 450)
	if e.state != stExplore {
		t.Fatal("should re-explore")
	}
	for tt := int64(450); tt <= 800; tt += 10 {
		e.Tick(tt)
	}
	e.Up(rx, ry, 800)
	if strings.Join(r.commits, ",") != "ら" {
		t.Fatalf("got %v", r.commits)
	}
}

func TestStandardMode(t *testing.T) {
	e, r := newTestEngine(false)
	x, y := keyCenter(e, "な")
	_, oy := dp(e, 0, 40)
	e.Down(x, y, 0)
	e.Up(x, y+oy, 2000) // 長く押してもフリックとして入力
	if strings.Join(r.commits, ",") != "の" {
		t.Fatalf("got %v", r.commits)
	}
}

func TestTypistIME(t *testing.T) {
	inj := &LogInjector{}
	ty := NewTypist(inj)
	ty.Configure(DefaultConfig().Keyboard)
	ty.SetKind("kana")
	ty.Commit("か")
	ty.Commit("{DAKU}")
	ty.Commit("つ")
	ty.Commit("{CYCLE}") // っ
	ty.Commit("ん")
	ops := strings.Join(inj.Ops, "|")
	want := "ime true|ascii ka|key []+0x8|ascii ga|ascii tu|key []+0x8|ascii ltu|ascii nn"
	if ops != want {
		t.Fatalf("\n got %s\nwant %s", ops, want)
	}
	if ty.History() != "がっん" {
		t.Fatalf("hist %q", ty.History())
	}
}

func TestTypistAlpha(t *testing.T) {
	inj := &LogInjector{}
	ty := NewTypist(inj)
	ty.Configure(DefaultConfig().Keyboard)
	ty.SetKind("alpha")
	ty.Commit("a")
	ty.Commit("{CASE}")
	ty.Commit("{CTRL+SHIFT+Z}")
	ops := strings.Join(inj.Ops, "|")
	want := "ime false|ascii a|key []+0x8|ascii A|key [17 16]+0x5a"
	if ops != want {
		t.Fatalf("\n got %s\nwant %s", ops, want)
	}
}

func TestCycles(t *testing.T) {
	seq := []rune{}
	r := 'は'
	for i := 0; i < 4; i++ {
		seq = append(seq, r)
		r = cycleRune(r)
	}
	if string(seq) != "はばぱは" {
		t.Fatalf("%s", string(seq))
	}
	seq = nil
	r = 'つ'
	for i := 0; i < 4; i++ {
		seq = append(seq, r)
		r = cycleRune(r)
	}
	if string(seq) != "つっづつ" {
		t.Fatalf("%s", string(seq))
	}
}

func TestParseCombo(t *testing.T) {
	m, vk, ok := ParseCombo("ALT+F4")
	if !ok || vk != 0x73 || len(m) != 1 {
		t.Fatal("alt+f4")
	}
	if _, _, ok := ParseCombo("FOO"); ok {
		t.Fatal("foo")
	}
}

func TestLayoutsValid(t *testing.T) {
	l := DefaultLayouts()
	for _, kb := range l.Keyboards {
		grid := map[[2]int]string{}
		for _, k := range kb.Keys {
			for x := int(k.X); x < int(k.X+k.W); x++ {
				for y := int(k.Y); y < int(k.Y+k.H); y++ {
					if o, ok := grid[[2]int{x, y}]; ok {
						t.Fatalf("%s: overlap %s/%s at %d,%d", kb.Name, o, k.Label, x, y)
					}
					grid[[2]int{x, y}] = k.Label
				}
			}
			for _, s := range []string{k.C, k.L, k.U, k.R, k.D} {
				if IsAction(s) {
					n := actionName(s)
					switch {
					case n == "CYCLE" || n == "DAKU" || n == "HANDAKU" || n == "SMALL" || n == "CASE" || n == "CAPS" ||
						n == "MODE_NEXT" || n == "MOUSE" || strings.HasPrefix(n, "MODE:"):
					default:
						if _, _, ok := ParseCombo(n); !ok {
							t.Fatalf("bad action %s", s)
						}
					}
				}
			}
		}
		if len(grid) != kb.Cols*kb.Rows {
			t.Fatalf("%s: grid not full %d", kb.Name, len(grid))
		}
	}
}
