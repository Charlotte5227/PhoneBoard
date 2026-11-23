package main

import (
	"fmt"
	"strings"
	"testing"
)

type dirRec struct{ ops []string }

func (r *dirRec) typeASCII(s string) { r.ops = append(r.ops, s) }
func (r *dirRec) key(mods []uint16, vk uint16) {
	n := map[uint16]string{vkTab: "TAB", vkBack: "BS", vkReturn: "ENTER", vkSpace: "SPACE", vkEscape: "ESC", 0x25: "LEFT", 0x26: "UP", 0x27: "RIGHT"}[vk]
	if len(mods) > 0 {
		n = "S+" + n
	}
	r.ops = append(r.ops, "<"+n+">")
}
func (r *dirRec) uni(s string)   { r.ops = append(r.ops, "u:"+s) }
func (r *dirRec) setIME(on bool) { r.ops = append(r.ops, fmt.Sprintf("ime:%v", on)) }
func (r *dirRec) snd(group string, semi int) {
	if group == SndWarn {
		r.ops = append(r.ops, "!warn")
	}
}
func (r *dirRec) echo(s string, warn bool) {}
func (r *dirRec) toast(s string)           {}
func (r *dirRec) sub(m string)             { r.ops = append(r.ops, "sub:"+m) }
func (r *dirRec) take() string             { s := strings.Join(r.ops, ","); r.ops = nil; return s }

func newDirT(t *testing.T) (*Dir, *dirRec) {
	rec := &dirRec{}
	r, err := NewRecognizer(strokesJSON)
	if err != nil {
		t.Fatal(err)
	}
	d := NewDir(rec, r)
	d.Enter()
	if got := rec.take(); got != "ime:true,sub:ja" {
		t.Fatalf("enter: %s", got)
	}
	return d, rec
}

func TestDirJapanese(t *testing.T) {
	d, rec := newDirT(t)
	L := func(dir int) { d.Flick("L", dir, false, "") }
	R := func(dir int) { d.Flick("R", dir, false, "") }
	// せ = さ行(↑) + え(→)、 あ = 右トン、 ん = 右↖
	L(dUp)
	R(dRight)
	R(dTap)
	R(dUL)
	if got := rec.take(); got != "se,a,nn" {
		t.Fatalf("kana: %s", got)
	}
	// が = か + あ + ↙、もう一度 ↙ で か に戻る
	L(dUL)
	R(dTap)
	R(dDL)
	R(dDL)
	if got := rec.take(); got != "ka,<BS>,ga,<BS>,ka" {
		t.Fatalf("dakuten: %s", got)
	}
	// つ → っ → づ
	L(dUR)
	R(dUp)
	R(dDL)
	R(dDL)
	if got := rec.take(); got != "tu,<BS>,xtu,<BS>,du" {
		t.Fatalf("tsu: %s", got)
	}
	// 記号: か+↖=「  は+↙=！  さ+↖=【（直接）  な+↙=空き
	L(dUL)
	R(dUL)
	L(dTap)
	R(dDL)
	L(dUp)
	R(dUL)
	L(dLeft)
	R(dDL)
	if got := rec.take(); got != "[,!,u:【,!warn" {
		t.Fatalf("symbols: %s", got)
	}
	// わ行・や行の空き枠: 、。・ ー、を
	for _, v := range []int{dLeft, dUp, dRight, dDown} {
		L(dDR)
		R(v)
	}
	L(dDL)
	R(dLeft)
	if got := rec.take(); got != ",,.,/,wo,-" {
		t.Fatalf("wa/ya: %s", got)
	}
}

func TestDirConvert(t *testing.T) {
	d, rec := newDirT(t)
	L := func(dir int) { d.Flick("L", dir, false, "") }
	R := func(dir int) { d.Flick("R", dir, false, "") }
	R(dUR) // 何も打っていない → 注意音
	if got := rec.take(); got != "!warn" {
		t.Fatalf("empty convert: %s", got)
	}
	L(dUL)
	R(dLeft) // き
	R(dUR)   // 変換
	R(dDown) // 次の候補
	R(dUp)   // 前の候補
	L(dRight)
	L(dLeft)  // 区切り
	R(dRight) // 確定
	if got := rec.take(); got != "ki,<SPACE>,<SPACE>,<UP>,<S+RIGHT>,<S+LEFT>,<ENTER>" {
		t.Fatalf("convert: %s", got)
	}
	// 変換中に新しく打つと変換モードを抜ける
	L(dUL)
	R(dTap)
	R(dUR)
	L(dUp)
	R(dTap)
	R(dLeft) // 変換モードではないので「い」
	if got := rec.take(); got != "ka,<SPACE>,sa,i" {
		t.Fatalf("leave convert: %s", got)
	}
	// や行の え（空白）: 入力途中なら確定してから
	L(dDL)
	R(dRight)
	if got := rec.take(); got != "<ENTER>,<SPACE>" {
		t.Fatalf("space: %s", got)
	}
	// ↖（ん）から曲げる: → 削除 / ↓ 確定 / ↑ Tab
	d.Flick("R", dUL, false, "bs")
	d.Flick("R", dUL, false, "enter")
	d.Flick("R", dUL, false, "tab")
	if got := rec.take(); got != "<BS>,<ENTER>,<TAB>" {
		t.Fatalf("long: %s", got)
	}
}

func TestDirSwitch(t *testing.T) {
	d, rec := newDirT(t)
	d.Flick("R", dDR, false, "")
	if got := rec.take(); got != "ime:false,sub:num" {
		t.Fatalf("to num: %s", got)
	}
	for _, v := range []int{dUL, dUp, dUR, dLeft, dTap, dRight, dDL, dDown, dDR} {
		d.Flick("L", v, false, "")
	}
	d.Flick("R", dTap, false, "")
	d.Flick("R", dUp, false, "")
	d.Flick("R", dDL, false, "")
	if got := rec.take(); got != "1,2,3,4,5,6,7,8,9,0,.,%" {
		t.Fatalf("digits: %s", got)
	}
	d.Flick("R", dUL, false, "bs") // 数字でも ↖ から曲げて削除
	if got := rec.take(); got != "<BS>" {
		t.Fatalf("num bs: %s", got)
	}
	d.Flick("R", dDR, false, "")
	if got := rec.take(); got != "ime:true,sub:ja" {
		t.Fatalf("back: %s", got)
	}
	d.Flick("R", dDR, false, "bend") // ↘ からそのまま左 → 英字
	if got := rec.take(); got != "ime:false,sub:en" {
		t.Fatalf("to en: %s", got)
	}
	d.Flick("L", dUp, false, "") // 英字でははじきは無視
	d.TwoTap()
	if got := rec.take(); got != "ime:true,sub:ja" {
		t.Fatalf("two tap: %s", got)
	}
}

func TestDirEnglish(t *testing.T) {
	d, rec := newDirT(t)
	d.Flick("R", dDR, false, "bend")
	rec.take()
	var f strokeFile
	if r, _ := NewRecognizer(strokesJSON); r == nil {
		t.Fatal("no recognizer")
	}
	_ = f
	shape := func(k string, scale float64) []pt {
		var out []pt
		for _, q := range defaultShapesForTest(t)[k] {
			out = append(out, pt{q[0]*scale + 7, q[1]*scale*1.1 + 3})
		}
		return out
	}
	d.Stroke(shape("H", 1.2), false)
	d.Stroke(shape("I", 0.8), true)
	d.Stroke(shape(" ", 1), false)
	d.Stroke(shape("BS", 1), false)
	if got := rec.take(); got != "h,I,<SPACE>,<BS>" {
		t.Fatalf("english: %s", got)
	}
	// どの見本とも遠い線は入れない（厳しめにして確認）
	d.rec.Threshold = 0.12
	d.Stroke([]pt{{0, 0}, {50, 3}, {2, 40}, {60, 45}, {5, 90}, {70, 2}, {0, 60}, {80, 80}, {10, 5}}, false)
	if got := rec.take(); got != "!warn" {
		t.Fatalf("reject: %s", got)
	}
}

func defaultShapesForTest(t *testing.T) map[string][][]float64 {
	var f strokeFile
	if err := jsonUnmarshal(strokesJSON, &f); err != nil {
		t.Fatal(err)
	}
	return f.DefaultShapes
}

// 一筆書きの見本どうしを判定させて、自分の見本で正しく読めるか（登録した書き方が他の文字に負けないか）
func TestStrokeSamplesSelfConsistent(t *testing.T) {
	r, _ := NewRecognizer(strokesJSON)
	var f strokeFile
	_ = jsonUnmarshal(strokesJSON, &f)
	bad := 0
	for k, L := range f.Letters {
		for _, s := range L.Samples {
			ms := r.Recognize(toPts(s))
			if len(ms) == 0 || ms[0].Key != k {
				bad++
				t.Logf("sample of %q read as %q", k, ms[0].Key)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d samples misread", bad)
	}
}
