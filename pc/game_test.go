package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

type nullSender struct{}

func (nullSender) Send(v any)   {}
func (nullSender) Close()       {}
func (nullSender) Name() string { return "test" }

func TestGameButtons(t *testing.T) {
	os.Setenv("PHONEBOARD_DATA", t.TempDir())
	inj := &LogInjector{}
	a := NewApp(DefaultConfig(), DefaultLayouts(), inj)
	s := nullSender{}
	a.OnConnect(s, PhoneInfo{}, "x")
	a.Handle(s, &Msg{T: "act", A: "{GAME}"})
	if a.mode != "game" {
		t.Fatalf("mode=%s", a.mode)
	}
	inj.Ops = nil
	a.Handle(s, &Msg{T: "btn", A: "{W}", S: "down"})
	a.Handle(s, &Msg{T: "btn", A: "{LSHIFT}", S: "down"})
	a.Handle(s, &Msg{T: "lm", Dx: 10, Dy: 0})
	a.Handle(s, &Msg{T: "btn", A: "{CLICK_L}", S: "down"})
	a.Handle(s, &Msg{T: "btn", A: "{CLICK_L}", S: "up"})
	a.Handle(s, &Msg{T: "btn", A: "{W}", S: "up"})
	a.Handle(s, &Msg{T: "btn", A: "{LSHIFT}", S: "up"})
	got := strings.Join(inj.Ops, ",")
	want := "down 0x57,down 0xa0,move 15,0,btn 0 true,btn 0 false,up 0x57,up 0xa0"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	// 視点パッドのタップ = 左クリック（少し押してから離す）
	inj.Ops = nil
	a.Handle(s, &Msg{T: "tap", N: 1, B: "l"})
	time.Sleep(80 * time.Millisecond)
	if got := strings.Join(inj.Ops, ","); got != "btn 0 true,btn 0 false" {
		t.Fatalf("tap: %s", got)
	}
	// 押したまま画面を切り替えたら離す
	inj.Ops = nil
	a.Handle(s, &Msg{T: "btn", A: "{D}", S: "down"})
	a.Handle(s, &Msg{T: "act", A: "{MOUSE}"})
	if got := strings.Join(inj.Ops, ","); !strings.Contains(got, "up 0x44") {
		t.Fatalf("not released: %s", got)
	}
}
