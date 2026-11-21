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

// KeyEvent は入力した1キー分の情報。
type KeyEvent struct {
	Mods   []uint16
	VK     uint16
	Name   string // 送ったキー名（A, SPACE, F1, ...）
	Label  string // 表示用
	Letter rune   // 英字なら小文字
	Index  int    // キー配置の何番目のキーか（音の高さ・表示用）
	Fn     bool   // Fn と一緒に押した
	Warn   string // 注意: "" / romaji（ローマ字として変な子音の並び）/ alpha（英字のまま日本語を打っていそう）
}

// KbdOut はキーボードエンジンからアプリへの出力。
type KbdOut interface {
	KeyPress(ev KeyEvent)        // キー入力（修飾キー付き）
	ModKey(vk uint16, down bool) // 同時押し方式: 修飾キーの押下/解放
	Action(a string)             // {MOUSE} など
	Feedback(kind string)        // hover / commit / mod / cancel（振動・音）
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
	changed bool    // 触れた後に別のキーへ移動した（探していた）
	prevKey int     // ひとつ前に乗っていたキー
	prevDur int64   // ひとつ前のキーに乗っていた時間
	chX     float64 // キーが変わる直前の位置
	chY     float64
}

// Calib はタッチ位置のクセ（左手側[0]・右手側[1]、キー1個=1の単位）。
type Calib struct {
	X [2]float64 `json:"x"`
	Y [2]float64 `json:"y"`
	N int        `json:"n"` // 学習した回数
}

type calibSample struct {
	side   int
	dx, dy float64
}

// Kbd は横向きの物理配列キーボードの判定を行う。PC側で判定するので、PC画面の表示と完全に一致する。
type Kbd struct {
	cfg      KeyboardCfg
	board    *Board
	out      KbdOut
	fingers  map[int]*finger
	state    map[string]modState // ワンショット方式の状態
	held     map[string]int      // 修飾キーを押さえている指の数
	used     map[string]bool     // 押さえている間に他のキーを打った（＝同時押しとして使った）
	ctx      []rune              // 入力補助用: 今の単語
	IME      func() bool         // IMEがONか
	flash    map[int]int64       // 入力したキー → 時刻（表示用）
	Calib    Calib               // タッチ位置の自動補正
	pending  *calibSample        // 直前に入力したキーのずれ（次がBSでなければ学習に使う）
	Learned  func()              // 学習した時（保存用）
	jaWarned bool                // 「英字のまま日本語」の注意を一度鳴らした（以後は鳴らさない）
}

// ResetJaWarn は「英字のまま日本語」の注意をまた鳴らせるようにする（IMEをONにした・アプリを切り替えた時）。
func (k *Kbd) ResetJaWarn() { k.jaWarned = false }

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

func (k *Kbd) side(x float64) int {
	if x < 0.5 {
		return 0
	}
	return 1
}

// pos は画面位置(0〜1)をキー単位の座標にし、タッチ位置のクセを補正する。
func (k *Kbd) pos(x, y float64) (float64, float64) {
	bx, by := x*k.board.Cols, y*k.board.Rows
	if k.cfg.AutoCalib {
		s := k.side(x)
		bx -= k.Calib.X[s]
		by -= k.Calib.Y[s]
	}
	return bx, by
}

// rawHit は位置(0〜1)を含むキー。
func (k *Kbd) rawHit(x, y float64) int {
	if k.board == nil {
		return -1
	}
	bx, by := k.pos(x, y)
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
	bx, by := k.pos(x, y)
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
	f := &finger{id: id, x: x, y: y, downT: t, keyT: t, prevKey: -1}
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
	px, py := f.x, f.y
	f.x, f.y = x, y
	if f.mod != "" || f.done {
		return
	}
	if h := k.Hit(x, y); h != f.key {
		f.prevKey, f.prevDur = f.key, t-f.keyT
		f.chX, f.chY = px, py
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
	// 離す瞬間の指の転がりで隣のキーに乗った場合は、直前まで乗っていたキーにする
	// （キーの外へ逃がした＝取り消し、や大きく動かした時は除く）
	if g := int64(k.cfg.SlipGuardMs); g > 0 && f.mod == "" && !f.done && f.prevKey >= 0 && f.key >= 0 &&
		t-f.keyT < g && f.prevDur >= g &&
		math.Hypot((f.x-f.chX)*k.board.Cols, (f.y-f.chY)*k.board.Rows) < 0.5 {
		f.key = f.prevKey
	}
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
	k.learn(f)
	k.commit(f.key)
}

// learn はタッチ位置のクセを学習する。
// 直前に入力したキーのずれは、次に打ったキーがBSでなければ（＝打ち間違いでなかったら）使う。
func (k *Kbd) learn(f *finger) {
	if !k.cfg.AutoCalib {
		k.pending = nil
		return
	}
	key := &k.board.Keys[f.key]
	if p := k.pending; p != nil && key.Key != "BS" {
		const rate = 0.06
		k.Calib.X[p.side] = clampF(k.Calib.X[p.side]+rate*p.dx, -0.35, 0.35)
		k.Calib.Y[p.side] = clampF(k.Calib.Y[p.side]+rate*p.dy, -0.35, 0.35)
		k.Calib.N++
		if k.Learned != nil {
			k.Learned()
		}
	}
	k.pending = nil
	// 普通の大きさの文字キーだけを使う（Space・Enter・Shiftなど大きいキーは中心がはっきりしないため）
	if key.Key == "" || key.Key == "BS" || key.Mod != "" || key.Action != "" || key.W > 1.3 || key.H > 1.3 || f.changed || k.fnActive() {
		return
	}
	bx, by := k.pos(f.x, f.y)
	dx := bx - (key.X + key.W/2)
	dy := by - (key.Y + key.H/2)
	if math.Abs(dx) > 0.7 || math.Abs(dy) > 0.7 {
		return
	}
	k.pending = &calibSample{side: k.side(f.x), dx: dx, dy: dy}
}

// ResetCalib はタッチ位置の補正を初期化する。
func (k *Kbd) ResetCalib() {
	k.Calib = Calib{}
	k.pending = nil
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
	onlyShift := len(mods) == 0 || (len(mods) == 1 && mods[0] == modVK["shift"])
	ev := KeyEvent{Mods: mods, VK: vk, Name: name, Label: label, Letter: letter, Index: i, Fn: fn && key.FnKey != ""}
	if letter != 0 && onlyShift {
		ev.Warn = k.checkWarn(string(k.ctx), letter, k.modActive("shift"))
	}
	k.out.KeyPress(ev)
	k.flash[i] = 1
	// ワンショットの修飾キーは1回使ったら解除
	for m, s := range k.state {
		if s == modOnce {
			k.state[m] = modOff
		}
	}
	// 入力補助の文脈
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

// checkWarn は注意音を鳴らすべきかを判定する（ctx はこの文字の前までの単語）。
func (k *Kbd) checkWarn(ctx string, ch rune, shift bool) string {
	ime := k.IME != nil && k.IME()
	if ime {
		// 日本語入力中: ローマ字として成り立たない子音の並び（例: kt, sd）
		if k.cfg.Warn && !shift && !isVowel(ch) && romajiPending(ctx) != "" && romajiPrior(ctx, ch) < 0.1 {
			return "romaji"
		}
		return ""
	}
	// 英字入力中: 辞書に無く、ローマ字としては自然な並びが続いた → 日本語のつもりかも（一度だけ）
	if !k.cfg.WarnAlpha || k.jaWarned {
		return ""
	}
	word := ctx + string(ch)
	if len(word) >= 6 && romajiLike(word) && freqPrefix(word) == 0 {
		k.jaWarned = true
		return "alpha"
	}
	return ""
}

// romajiLike は文字列全体がローマ字として自然に読めるか。
func romajiLike(w string) bool {
	vowels := 0
	for i := 0; i < len(w); i++ {
		if romajiPrior(w[:i], rune(w[i])) < 0.3 {
			return false
		}
		if isVowel(rune(w[i])) {
			vowels++
		}
	}
	return vowels*3 >= len(w)
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
