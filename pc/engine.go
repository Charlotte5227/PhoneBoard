package main

import "math"

// Dir はフリック方向。
type Dir int

const (
	DirC Dir = iota
	DirL
	DirU
	DirR
	DirD
)

func (d Dir) String() string {
	return [...]string{"c", "l", "u", "r", "d"}[d]
}

type engState int

const (
	stIdle engState = iota
	stExplore
	stArmed
)

// EngineEvent は判定エンジンからの通知。
type EngineEvent struct {
	Kind   string // "haptic" / "commit"
	Haptic string // hover / arm / dir / commit / cancel
	Text   string // commit する出力（文字列 or {ACTION}）
	Key    string // 対象キーのラベル
}

// EngineSnap はオーバーレイ描画用の状態。
type EngineSnap struct {
	Touching bool
	Armed    bool
	Hover    int // 指の下のキー (-1: なし)
	ArmKey   int // 確定待機中のキー
	Dir      Dir
	RX, RY   float64 // キーボード領域内の位置 (0〜1, 範囲外もあり)
	Inside   bool
	Canceled bool // 直前のタッチが取り消された
}

// Engine は「探って・止めて・はじく」片手フリック入力の判定を行う。
//
//   - 指を置いてなぞる → 指の下のキーがPC画面上で光る（探り中、まだ入力されない）
//   - 指を止める(DwellMs) → 振動して「確定待機」。そこからはじいた方向の文字を、離した時に入力
//   - 慣れた人向け: 置いてすぐ(QuickMs以内)はじいて離せば普通のフリックとして入力
//   - 探り中に長く触ってから離した場合は何も入力しない（取り消し）
type Engine struct {
	cfg    KeyboardCfg
	lay    *KbLayout
	reg    Region
	wdp    float64
	hdp    float64
	emit   func(EngineEvent)
	state  engState
	downX  float64
	downY  float64
	downT  int64
	curX   float64
	curY   float64
	hover  int
	stillX float64
	stillY float64
	stillT int64
	armKey int
	ancX   float64
	ancY   float64
	armT   int64
	dir    Dir
	reps   int
	lastRp int64
	cancel bool
}

func NewEngine(emit func(EngineEvent)) *Engine {
	return &Engine{emit: emit, hover: -1, armKey: -1, wdp: 400, hdp: 800}
}

func (e *Engine) Configure(cfg KeyboardCfg, lay *KbLayout, reg Region, wdp, hdp float64) {
	e.cfg = cfg
	e.lay = lay
	e.reg = reg
	if wdp > 0 {
		e.wdp = wdp
	}
	if hdp > 0 {
		e.hdp = hdp
	}
	if e.state != stIdle {
		e.reset()
	}
}

func (e *Engine) reset() {
	e.state = stIdle
	e.hover = -1
	e.armKey = -1
	e.dir = DirC
	e.reps = 0
}

// HitTest は画面座標(0〜1)にあるキーの番号を返す。
func (e *Engine) HitTest(x, y float64) int {
	if e.lay == nil || e.reg.W <= 0 || e.reg.H <= 0 {
		return -1
	}
	gx := (x - e.reg.X) / e.reg.W * float64(e.lay.Cols)
	gy := (y - e.reg.Y) / e.reg.H * float64(e.lay.Rows)
	for i := len(e.lay.Keys) - 1; i >= 0; i-- {
		k := &e.lay.Keys[i]
		if gx >= k.X && gx < k.X+k.W && gy >= k.Y && gy < k.Y+k.H {
			return i
		}
	}
	return -1
}

func (e *Engine) distDp(x1, y1, x2, y2 float64) float64 {
	dx := (x2 - x1) * e.wdp
	dy := (y2 - y1) * e.hdp
	return math.Hypot(dx, dy)
}

func (e *Engine) dirOf(x1, y1, x2, y2 float64) Dir {
	dx := (x2 - x1) * e.wdp
	dy := (y2 - y1) * e.hdp
	if math.Hypot(dx, dy) < e.cfg.FlickDp {
		return DirC
	}
	if math.Abs(dx) > math.Abs(dy) {
		if dx < 0 {
			return DirL
		}
		return DirR
	}
	if dy < 0 {
		return DirU
	}
	return DirD
}

func (e *Engine) haptic(kind string) {
	e.emit(EngineEvent{Kind: "haptic", Haptic: kind})
}

func (e *Engine) commit(ki int, d Dir) {
	if e.lay == nil || ki < 0 || ki >= len(e.lay.Keys) {
		return
	}
	k := &e.lay.Keys[ki]
	s := k.Slot(d)
	if s == "" {
		e.haptic("cancel")
		return
	}
	e.emit(EngineEvent{Kind: "commit", Text: s, Key: k.Label})
	e.haptic("commit")
}

func (e *Engine) arm(t int64) {
	e.state = stArmed
	e.armKey = e.hover
	e.ancX, e.ancY = e.curX, e.curY
	e.armT = t
	e.dir = DirC
	e.reps = 0
	e.haptic("arm")
}

func (e *Engine) Down(x, y float64, t int64) {
	e.reset()
	e.cancel = false
	e.downX, e.downY, e.downT = x, y, t
	e.curX, e.curY = x, y
	e.stillX, e.stillY, e.stillT = x, y, t
	e.hover = e.HitTest(x, y)
	if !e.cfg.Explore {
		e.state = stArmed
		e.armKey = e.hover
		e.ancX, e.ancY, e.armT = x, y, t
		return
	}
	e.state = stExplore
	if e.hover >= 0 {
		e.haptic("hover")
	}
}

func (e *Engine) Move(x, y float64, t int64) {
	if e.state == stIdle {
		return
	}
	e.curX, e.curY = x, y
	switch e.state {
	case stExplore:
		h := e.HitTest(x, y)
		if h != e.hover {
			e.hover = h
			if h >= 0 {
				e.haptic("hover")
			}
		}
		if e.distDp(e.stillX, e.stillY, x, y) > e.cfg.StillDp {
			e.stillX, e.stillY, e.stillT = x, y, t
		}
	case stArmed:
		if e.cfg.Explore && e.cfg.RearmDp > 0 && e.distDp(e.ancX, e.ancY, x, y) > e.cfg.RearmDp {
			// 大きく動かしたので探り直し
			e.state = stExplore
			e.armKey = -1
			e.dir = DirC
			e.hover = e.HitTest(x, y)
			e.stillX, e.stillY, e.stillT = x, y, t
			e.haptic("cancel")
			return
		}
		d := e.dirOf(e.ancX, e.ancY, x, y)
		if d != e.dir {
			e.dir = d
			if e.lay != nil && e.armKey >= 0 && e.lay.Keys[e.armKey].Slot(d) != "" {
				e.haptic("dir")
			}
		}
	}
}

// Tick は指が止まっている間の時間経過処理（確定待機・キーリピート）。
func (e *Engine) Tick(t int64) {
	switch e.state {
	case stExplore:
		if e.hover >= 0 && t-e.stillT >= int64(e.cfg.DwellMs) {
			e.arm(t)
		}
	case stArmed:
		if e.lay == nil || e.armKey < 0 || e.dir != DirC {
			return
		}
		k := &e.lay.Keys[e.armKey]
		if !k.Repeat {
			return
		}
		if e.reps == 0 {
			if t-e.armT >= int64(e.cfg.RepeatDelayMs) {
				e.commit(e.armKey, DirC)
				e.reps = 1
				e.lastRp = t
			}
		} else if t-e.lastRp >= int64(e.cfg.RepeatIntervalMs) {
			e.commit(e.armKey, DirC)
			e.reps++
			e.lastRp = t
		}
	}
}

func (e *Engine) Up(x, y float64, t int64) {
	if e.state == stIdle {
		return
	}
	e.Move(x, y, t)
	switch e.state {
	case stArmed:
		if !(e.reps > 0 && e.dir == DirC) {
			e.commit(e.armKey, e.dir)
		}
	case stExplore:
		if t-e.downT <= int64(e.cfg.QuickMs) {
			// 素早い通常フリック/タップ
			ki := e.HitTest(e.downX, e.downY)
			if ki >= 0 {
				e.commit(ki, e.dirOf(e.downX, e.downY, x, y))
			}
		} else {
			e.cancel = true
			e.haptic("cancel")
		}
	}
	e.reset()
}

func (e *Engine) Cancel() {
	e.reset()
}

func (e *Engine) Snap() EngineSnap {
	s := EngineSnap{
		Touching: e.state != stIdle,
		Armed:    e.state == stArmed,
		Hover:    e.hover,
		ArmKey:   e.armKey,
		Dir:      e.dir,
		Canceled: e.cancel,
	}
	if e.reg.W > 0 && e.reg.H > 0 {
		s.RX = (e.curX - e.reg.X) / e.reg.W
		s.RY = (e.curY - e.reg.Y) / e.reg.H
		s.Inside = s.RX >= 0 && s.RX < 1 && s.RY >= 0 && s.RY < 1
	}
	return s
}
