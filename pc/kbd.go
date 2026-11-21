package main

import (
	"math"
	"sort"
	"strings"
)

// 修飾キーの状態（ワンショット方式）
type modState int

const (
	modOff  modState = iota
	modOnce          // 次のキー1回だけ
	modLock          // 固定
)

// KbdOut はキーボードエンジンからアプリへの出力。
type KbdOut interface {
	KeyPress(mods []uint16, vk uint16, label string, letter rune) // キー入力（修飾キー付き）
	ModKey(vk uint16, down bool)                                  // 同時押し方式: 修飾キーの押下/解放
	Action(a string)                                              // {MOUSE} など
	Feedback(kind string)                                         // hover / commit / mod / cancel（振動・音）
}

type finger struct {
	id      int
	x, y    float64 // 画面上の位置(0〜1)
	key     int     // 今判定されているキー
	keyT    int64   // そのキーに乗った時刻
	mod     string  // 修飾キーを押さえている指なら修飾キー名
	done    bool    // 入力済み（触れた瞬間方式やリピート）
	reps    int
	lastRep int64
	downT   int64
	changed bool // 触れた後に別のキーへ移動した（探していた）
}

// Kbd は横向きの物理配列キーボードの判定を行う。PC側で判定するので、PC画面の表示と完全に一致する。
type Kbd struct {
	cfg     KeyboardCfg
	board   *Board
	out     KbdOut
	fingers map[int]*finger
	state   map[string]modState // ワンショット方式の状態
	held    map[string]int      // 修飾キーを押さえている指の数
	used    map[string]bool     // 押さえている間に他のキーを打った（＝同時押しとして使った）
	ctx     []rune              // 入力補助用: 今の単語
	IME     func() bool         // IMEがONか
	flash   map[int]int64       // 入力したキー → 時刻（表示用）
}

func NewKbd(out KbdOut) *Kbd {
	return &Kbd{out: out, fingers: map[int]*finger{}, state: map[string]modState{}, held: map[string]int{},
		used: map[string]bool{}, flash: map[int]int64{}}
}

func (k *Kbd) Configure(cfg KeyboardCfg, b *Board) {
	k.CancelAll()
	k.cfg = cfg
	k.board = b
	if cfg.ModMode == "hold" {
		k.state = map[string]modState{}
	}
}

func (k *Kbd) sticky() bool { return k.cfg.ModMode != "hold" }

// modActive はその修飾キーが今効いているか。
func (k *Kbd) modActive(m string) bool {
	return k.held[m] > 0 || k.state[m] != modOff
}

func (k *Kbd) fnActive() bool { return k.modActive("fn") }

// rawHit は位置(0〜1)を含むキー。
func (k *Kbd) rawHit(x, y float64) int {
	if k.board == nil {
		return -1
	}
	bx, by := x*k.board.Cols, y*k.board.Rows
	for i := len(k.board.Keys) - 1; i >= 0; i-- {
		key := &k.board.Keys[i]
		if bx >= key.X && bx < key.X+key.W && by >= key.Y && by < key.Y+key.H {
			return i
		}
	}
	return -1
}

// Hit は入力補助込みで、その位置で入力されるキーを決める。
func (k *Kbd) Hit(x, y float64) int {
	k0 := k.rawHit(x, y)
	if k0 < 0 || !k.cfg.Assist || k.cfg.AssistStrength <= 0 {
		return k0
	}
	key0 := &k.board.Keys[k0]
	if key0.Letter() == 0 || k.modActive("ctrl") || k.modActive("alt") || k.modActive("win") || k.fnActive() {
		return k0
	}
	bx, by := x*k.board.Cols, y*k.board.Rows
	// キーの中心付近をしっかり押した時は、そのまま
	if math.Abs(bx-(key0.X+key0.W/2)) < key0.W*0.22 && math.Abs(by-(key0.Y+key0.H/2)) < key0.H*0.22 {
		return k0
	}
	ime := k.IME != nil && k.IME()
	ctx := string(k.ctx)
	best, bestScore := k0, -1.0
	const sigma = 0.45
	for i := range k.board.Keys {
		key := &k.board.Keys[i]
		ch := key.Letter()
		if ch == 0 {
			continue
		}
		dx := (bx - (key.X + key.W/2)) / key.W
		dy := (by - (key.Y + key.H/2)) / key.H
		if math.Abs(dx) > 1.1 || math.Abs(dy) > 1.1 {
			continue
		}
		spatial := math.Exp(-(dx*dx + dy*dy) / (2 * sigma * sigma))
		score := spatial * math.Pow(Prior(ctx, ch, ime), k.cfg.AssistStrength)
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return best
}

func (k *Kbd) Down(id int, x, y float64, t int64) {
	if old, ok := k.fingers[id]; ok {
		k.release(old, false)
	}
	f := &finger{id: id, x: x, y: y, downT: t, keyT: t}
	f.key = k.Hit(x, y)
	k.fingers[id] = f
	if f.key < 0 {
		return
	}
	key := &k.board.Keys[f.key]
	if key.Mod != "" && !(k.fnActive() && key.FnKey != "" && key.Mod != "fn") {
		f.mod = key.Mod
		f.done = true
		k.held[f.mod]++
		k.used[f.mod] = false
		if !k.sticky() && f.mod != "fn" && k.held[f.mod] == 1 {
			k.out.ModKey(modVK[f.mod], true)
		}
		k.out.Feedback("mod")
		return
	}
	k.out.Feedback("hover")
	if k.cfg.CommitOn == "press" {
		k.commit(f.key)
		f.done = true
	}
}

func (k *Kbd) Move(id int, x, y float64, t int64) {
	f, ok := k.fingers[id]
	if !ok {
		return
	}
	f.x, f.y = x, y
	if f.mod != "" || f.done {
		return
	}
	if h := k.Hit(x, y); h != f.key {
		f.key = h
		f.keyT = t
		f.changed = true
		if h >= 0 {
			k.out.Feedback("hover")
		}
	}
}

func (k *Kbd) Up(id int, x, y float64, t int64) {
	f, ok := k.fingers[id]
	if !ok {
		return
	}
	k.Move(id, x, y, t)
	k.release(f, true)
}

func (k *Kbd) release(f *finger, commit bool) {
	delete(k.fingers, f.id)
	if f.mod != "" {
		k.held[f.mod]--
		if k.held[f.mod] <= 0 {
			k.held[f.mod] = 0
			if !k.sticky() && f.mod != "fn" {
				k.out.ModKey(modVK[f.mod], false)
			}
			if k.sticky() && commit && !k.used[f.mod] {
				// 単独でタップ → ワンショット → 固定 → 解除
				switch k.state[f.mod] {
				case modOff:
					k.state[f.mod] = modOnce
				case modOnce:
					k.state[f.mod] = modLock
				default:
					k.state[f.mod] = modOff
				}
			}
			k.used[f.mod] = false
		}
		return
	}
	if !commit || f.done || f.reps > 0 {
		return
	}
	if f.key < 0 {
		k.out.Feedback("cancel") // キーの無い所（最上段の空き）で離した＝取り消し
		return
	}
	k.commit(f.key)
}

// commit はキーを入力する。
func (k *Kbd) commit(i int) {
	key := &k.board.Keys[i]
	fn := k.fnActive()
	if key.Action != "" && !(fn && key.FnKey != "") {
		k.out.Action(key.Action)
		k.flash[i] = 1
		return
	}
	name := key.Key
	label := key.Label
	if fn && key.FnKey != "" {
		name = key.FnKey
		if key.FnLabel != "" {
			label = key.FnLabel
		}
	}
	_, vk, ok := ParseCombo(name)
	if !ok {
		return
	}
	var mods []uint16
	for _, m := range []string{"ctrl", "alt", "win", "shift"} {
		if !k.modActive(m) {
			continue
		}
		if k.held[m] > 0 {
			k.used[m] = true
			if !k.sticky() {
				continue // 同時押し方式ではOS側で既に押されている
			}
		}
		mods = append(mods, modVK[m])
	}
	if k.held["fn"] > 0 {
		k.used["fn"] = true
	}
	letter := key.Letter()
	if fn && key.FnKey != "" {
		letter = 0
	}
	if letter != 0 && k.modActive("shift") {
		label = strings.ToUpper(label)
	} else if letter != 0 {
		label = strings.ToLower(label)
	} else if k.modActive("shift") && key.Shift != "" && !fn {
		label = key.Shift
	}
	k.out.KeyPress(mods, vk, label, letter)
	k.flash[i] = 1
	// ワンショットの修飾キーは1回使ったら解除
	for m, s := range k.state {
		if s == modOnce {
			k.state[m] = modOff
		}
	}
	// 入力補助の文脈
	onlyShift := len(mods) == 0 || (len(mods) == 1 && mods[0] == modVK["shift"])
	switch {
	case letter != 0 && onlyShift:
		k.ctx = append(k.ctx, letter)
		if len(k.ctx) > 30 {
			k.ctx = k.ctx[len(k.ctx)-30:]
		}
	case name == "BS" && len(k.ctx) > 0:
		k.ctx = k.ctx[:len(k.ctx)-1]
	default:
		k.ctx = k.ctx[:0]
	}
}

// ResetContext は単語の区切り（クリックやマウス操作など）で呼ぶ。
func (k *Kbd) ResetContext() { k.ctx = k.ctx[:0] }

// Tick はリピート処理（押し続けると連続入力）。
func (k *Kbd) Tick(t int64) {
	if k.board == nil {
		return
	}
	for _, f := range k.fingers {
		// なぞって探してきたキーではリピートしない（探している途中で消してしまわないように）
		if f.mod != "" || f.key < 0 || f.changed || !k.board.Keys[f.key].Repeat {
			continue
		}
		if f.reps == 0 {
			if t-f.downT >= int64(k.cfg.RepeatDelayMs) {
				k.commit(f.key)
				f.reps = 1
				f.lastRep = t
			}
		} else if t-f.lastRep >= int64(k.cfg.RepeatIntervalMs) {
			k.commit(f.key)
			f.reps++
			f.lastRep = t
		}
	}
}

// CancelAll は全ての指と押しっぱなしの修飾キーを解放する。
func (k *Kbd) CancelAll() {
	for _, f := range k.fingers {
		k.release(f, false)
	}
	k.fingers = map[int]*finger{}
	for m, n := range k.held {
		if n > 0 && !k.sticky() && m != "fn" {
			k.out.ModKey(modVK[m], false)
		}
	}
	k.held = map[string]int{}
}

// ===== 表示用 =====

type FingerView struct {
	X, Y float64
	Key  int
}

type KbdView struct {
	Fingers []FingerView
	Mods    map[string]string // off / once / lock / held
	Fn      bool
	Shift   bool
}

func (k *Kbd) View() KbdView {
	v := KbdView{Mods: map[string]string{}, Fn: k.fnActive(), Shift: k.modActive("shift")}
	ids := make([]int, 0, len(k.fingers))
	for id := range k.fingers {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		f := k.fingers[id]
		v.Fingers = append(v.Fingers, FingerView{f.x, f.y, f.key})
	}
	for _, m := range []string{"shift", "ctrl", "alt", "win", "fn"} {
		switch {
		case k.held[m] > 0:
			v.Mods[m] = "held"
		case k.state[m] == modLock:
			v.Mods[m] = "lock"
		case k.state[m] == modOnce:
			v.Mods[m] = "once"
		default:
			v.Mods[m] = "off"
		}
	}
	return v
}

// TakeFlash は入力されたキーの一覧を返して消す（表示の点滅用）。
func (k *Kbd) TakeFlash() []int {
	var out []int
	for i := range k.flash {
		out = append(out, i)
	}
	k.flash = map[int]int64{}
	return out
}
