package main

import (
	"strings"
	"unicode"
)

// ===== 方向入力（画面を見ずに、はじく方向で打つ） =====
//
// 日本語: 左の親指で行（子音）、右の親指で段（母音）。
// 数字:   左の親指でテンキー、右の親指で 0 と記号。
// 英字:   画面全体に一筆書き。
// 右の ↘ はどの画面でも切り替え口（そのまま左へ引くと英字）。英字からは両手の親指で同時にトン。

// 方向の番号（スマホから届く）: 0=トン(中央) 1=↑ 2=↗ 3=→ 4=↘ 5=↓ 6=↙ 7=← 8=↖
const (
	dTap = iota
	dUp
	dUR
	dRight
	dDR
	dDown
	dDL
	dLeft
	dUL
)

// dirOut は方向入力の出力先（App。テストでは記録係）。
type dirOut interface {
	typeASCII(s string)           // 今のキーボード配列のキーで打つ（IMEがONならローマ字入力）
	key(mods []uint16, vk uint16) // キー1つ（修飾キー付き）
	uni(s string)                 // 文字を直接送る
	setIME(on bool)               // 日本語入力のON/OFF
	snd(group string, semi int)   // 音
	echo(s string, warn bool)     // 画面中央に一瞬表示
	toast(s string)               // 画面の通知
	sub(m, in string)             // スマホへ今の画面（ja/num/en）と受け付け方（flick/stroke）を知らせる
}

var (
	dirRowOf   = map[int]int{dUL: 0, dUp: 1, dUR: 2, dLeft: 3, dTap: 4, dRight: 5, dDL: 6, dDown: 7, dDR: 8}
	dirVowelOf = map[int]int{dTap: 0, dLeft: 1, dUp: 2, dRight: 3, dDown: 4}
	dirRowCons = []string{"k", "s", "t", "n", "h", "m", "y", "r", "w"}
	dirRowName = []rune("かさたなはまやらわ")
	dirVowels  = "aiueo"
	dirKana    = [9]string{"かきくけこ", "さしすせそ", "たちつてと", "なにぬねの", "はひふへほ", "まみむめも", "や゛ゆ゛よ", "らりるれろ", "わ゛゛゛を"}
	dirNumL    = map[int]string{dUL: "1", dUp: "2", dUR: "3", dLeft: "4", dTap: "5", dRight: "6", dDL: "7", dDown: "8", dDR: "9"}
	dirNumR    = map[int]string{dTap: "0", dUp: ".", dDown: ",", dLeft: "-", dRight: "+", dUL: "/", dUR: ":", dDL: "%"}
)

// dirSym は記号1つの送り方。ascii があればIMEを通す（入力途中の文字と一緒に変換できる）。
type dirSym struct{ ascii, uni, label string }

// 子音を選んでから右の ↖（開き）/ ↙（閉じ）
var dirSyms = [9][2]dirSym{
	{{ascii: "[", label: "「"}, {ascii: "]", label: "」"}},   // か: かぎかっこ
	{{uni: "【", label: "【"}, {uni: "】", label: "】"}},       // さ: すみつきかっこ
	{{ascii: "z.", label: "…"}, {ascii: ":", label: "："}},  // た: てんてん
	{{ascii: "~", label: "〜"}, {}},                         // な: なみ
	{{ascii: "?", label: "？"}, {ascii: "!", label: "！"}},   // は: はてな・びっくり
	{{ascii: "(", label: "（"}, {ascii: ")", label: "）"}},   // ま: まるかっこ
	{{ascii: "zl", label: "→"}, {ascii: "zh", label: "←"}}, // や: やじるし
	{{uni: "♪", label: "♪"}, {}},                           // ら: らら♪
	{{uni: "ｗ", label: "ｗ"}, {uni: "（笑）", label: "（笑）"}},   // わ: わら
}

// 濁点・小文字の切り替え（↙を押すたびに次へ）
var dirCycles = []string{
	"かが", "きぎ", "くぐ", "けげ", "こご", "さざ", "しじ", "すず", "せぜ", "そぞ",
	"ただ", "ちぢ", "つっづ", "てで", "とど", "はばぱ", "ひびぴ", "ふぶぷ", "へべぺ", "ほぼぽ",
	"あぁ", "いぃ", "うぅ", "えぇ", "おぉ", "やゃ", "ゆゅ", "よょ", "わゎ",
}

// 打ち直す時のローマ字
var dirRoma = map[rune]string{}

func init() {
	for r, row := range dirKana {
		for v, k := range []rune(row) {
			if k != '゛' {
				dirRoma[k] = dirRowCons[r] + string(dirVowels[v])
			}
		}
	}
	for v, k := range []rune("あいうえお") {
		dirRoma[k] = string(dirVowels[v])
	}
	dirRoma['を'] = "wo"
	extra := map[string]string{
		"が": "ga", "ぎ": "gi", "ぐ": "gu", "げ": "ge", "ご": "go", "ざ": "za", "じ": "zi", "ず": "zu", "ぜ": "ze", "ぞ": "zo",
		"だ": "da", "ぢ": "di", "づ": "du", "で": "de", "ど": "do", "ば": "ba", "び": "bi", "ぶ": "bu", "べ": "be", "ぼ": "bo",
		"ぱ": "pa", "ぴ": "pi", "ぷ": "pu", "ぺ": "pe", "ぽ": "po", "っ": "xtu", "ぁ": "xa", "ぃ": "xi", "ぅ": "xu", "ぇ": "xe", "ぉ": "xo",
		"ゃ": "xya", "ゅ": "xyu", "ょ": "xyo", "ゎ": "xwa", "ん": "nn",
	}
	for k, v := range extra {
		dirRoma[[]rune(k)[0]] = v
	}
}

type Dir struct {
	out  dirOut
	rec  *Recognizer
	Sub  string // ja / num / en
	row  int    // 選んだ行（-1 = なし）
	conv bool   // 変換モード
	comp int    // 入力途中（未確定）の文字数の目安
	last rune   // 直前に打ったかな（濁点・小文字の切り替え用）
	word []rune // 英字: 今の単語（迷った時の判断用）

	mods      []string // 付けている修飾キー（ctrl / shift / win）。次の1文字に付く
	selecting bool     // 音量↓を押して修飾キーを選んでいるところ
	repMods   []uint16 // 矢印を押さえ続けている間の修飾キー
	repLab    string
	repSet    bool
}

var (
	modKeyVK   = map[string]uint16{"ctrl": 0xA2, "shift": 0xA0, "win": 0x5B}
	modKeyName = map[string]string{"ctrl": "Ctrl", "shift": "Shift", "win": "Win"}
	// ↙ から曲げる（または選択中に ↑ → ↓ へはじく）
	modBend = map[string]string{"mshift": "shift", "mwin": "win", "mctrl": "ctrl"}
	modDir  = map[int]string{dUp: "shift", dRight: "win", dDown: "ctrl"}
)

// inputKind はスマホ側の受け付け方: 英字（または修飾キーの後の1文字）は一筆書き、それ以外ははじき。
func (d *Dir) inputKind() string {
	if d.selecting {
		return "flick"
	}
	if d.Sub == "en" || (len(d.mods) > 0 && d.Sub != "cur") { // 矢印画面では修飾キーは次の矢印に付く
		return "stroke"
	}
	return "flick"
}

func (d *Dir) notify() { d.out.sub(d.Sub, d.inputKind()) }

func (d *Dir) modLabel() string {
	s := ""
	for _, m := range d.mods {
		s += modKeyName[m] + "+"
	}
	return s
}

// ModSelect は音量↓（{MODKEY}）: 修飾キーの選択を始める／やめる。
func (d *Dir) ModSelect() {
	d.selecting = !d.selecting
	d.row = -1
	if d.selecting {
		d.out.snd(SndMod, 2)
		d.out.echo("修飾キー？", false)
		d.out.toast("修飾キー: ↑Shift →Win ↓Ctrl（トン=全部取り消し）")
	} else {
		d.out.snd(SndFunc, 2)
		d.out.echo(d.modLabel(), false)
	}
	d.notify()
}

// modChoose は選択中のはじき1回。同じキーを選ぶと外す（撤回）。
func (d *Dir) modChoose(dir int, bend string) {
	d.selecting = false
	m, ok := modBend[bend]
	if !ok {
		m, ok = modDir[dir]
	}
	switch {
	case dir == dTap && bend == "":
		d.mods = nil
		d.out.snd(SndFunc, 1)
		d.out.echo("修飾キー 取消", false)
		d.out.toast("修飾キーなし")
	case !ok:
		d.warn()
	default:
		removed := false
		for i, x := range d.mods {
			if x == m {
				d.mods = append(d.mods[:i], d.mods[i+1:]...)
				removed = true
				break
			}
		}
		if removed {
			d.out.snd(SndFunc, 3)
			d.out.echo(modKeyName[m]+" 取消", false)
		} else {
			d.mods = append(d.mods, m)
			d.out.snd(SndMod, 9)
			d.out.echo(d.modLabel(), false)
		}
		if len(d.mods) > 0 {
			if d.Sub == "cur" {
				d.out.toast(d.modLabel() + "（次の矢印）")
			} else {
				d.out.toast(d.modLabel() + "（次の1文字を一筆書き）")
			}
		} else {
			d.out.toast("修飾キーなし")
		}
	}
	d.notify()
}

func (d *Dir) modVKs(upper bool) []uint16 {
	var vs []uint16
	for _, m := range d.mods {
		vs = append(vs, modKeyVK[m])
	}
	if upper && !d.hasMod("shift") {
		vs = append(vs, modKeyVK["shift"])
	}
	return vs
}

func (d *Dir) hasMod(m string) bool {
	for _, x := range d.mods {
		if x == m {
			return true
		}
	}
	return false
}

// useMods は修飾キー付きでキーを1つ押して、修飾キーを外す。
func (d *Dir) useMods(vk uint16, label string, upper bool) {
	vs := d.modVKs(upper)
	d.out.key(vs, vk)
	lab := d.modLabel()
	if upper && !d.hasMod("shift") {
		lab += "Shift+"
	}
	d.mods = nil
	d.out.snd(SndMod, 6)
	d.out.echo(lab+label, false)
	d.notify()
}

// SingleTap は一筆書きの画面でのトン（修飾キーを付けている時だけ: そのキー単体を押す。Win だけでスタートなど）。
func (d *Dir) SingleTap() {
	if len(d.mods) == 0 || d.selecting {
		return
	}
	vs := d.modVKs(false)
	last := vs[len(vs)-1]
	d.out.key(vs[:len(vs)-1], last)
	d.out.echo(strings.TrimSuffix(d.modLabel(), "+"), false)
	d.out.snd(SndMod, 6)
	d.mods = nil
	d.notify()
}

func NewDir(out dirOut, rec *Recognizer) *Dir {
	return &Dir{out: out, rec: rec, Sub: "ja", row: -1}
}

// Enter は方向入力の画面に入った時（日本語から始める）。
func (d *Dir) Enter() { d.setSub("ja", false) }

func (d *Dir) clear() {
	d.row, d.conv, d.comp, d.last, d.word = -1, false, 0, 0, nil
	d.mods, d.selecting = nil, false
}

func (d *Dir) setSub(m string, sound bool) {
	d.clear()
	d.Sub = m
	d.out.setIME(m == "ja")
	d.notify()
	name := map[string]string{"ja": "日本語", "num": "数字", "en": "英字", "cur": "矢印"}[m]
	d.out.toast(name)
	if sound {
		d.out.snd(SndMod, map[string]int{"ja": 4, "num": 7, "en": 11, "cur": 1}[m])
	}
}

func (d *Dir) warn() { d.out.snd(SndWarn, 0) }

// 曲げ（右の ↖ から指を離さずに → ↓ ↑）で出す操作
var dirBendFn = map[string]int{"bs": dLeft, "enter": dDown, "tab": dRight}

// Preview は右の親指が切り替え口（↘）に入った時・そこから左へ曲げた時、
// ↖ から曲げた時（音で知らせる。離す前に戻せば取り消し）。
func (d *Dir) Preview(side string, dir int, bend string) {
	if m, ok := modBend[bend]; ok {
		if d.selecting {
			d.out.snd(SndMod, 9)
			d.out.echo(modKeyName[m]+"？", false)
		}
		return
	}
	if side != "R" || d.Sub == "en" {
		return
	}
	switch bend {
	case "bs":
		d.out.snd(SndHeavy, 2)
		d.out.echo("削除？", false)
		return
	case "enter":
		d.out.snd(SndHeavy, 8)
		d.out.echo("確定？", false)
		return
	case "tab":
		d.out.snd(SndHeavy, 11)
		d.out.echo("Tab？", false)
		return
	}
	if dir != dDR {
		return
	}
	switch {
	case bend == "bend":
		d.out.snd(SndMod, 11)
		d.out.echo("英字", false)
	case bend == "bendup":
		d.out.snd(SndMod, 1)
		d.out.echo("矢印", false)
	case d.Sub == "ja":
		d.out.snd(SndMod, 7)
		d.out.echo("数字", false)
	default:
		d.out.snd(SndMod, 4)
		d.out.echo("日本語", false)
	}
}

// Flick ははじき1回（指を離した時）。
// bend: "bend"=↘から左へ, "bendup"=↘から上へ, "bs"/"enter"/"tab"=↖から曲げた, "m…"=↙から曲げた
// rep: 矢印画面で押さえ続けた時の2回目以降
func (d *Dir) Flick(side string, dir int, rep bool, bend string) {
	if dir < 0 || dir > 8 {
		return
	}
	if d.selecting {
		d.modChoose(dir, bend)
		return
	}
	if _, ok := modBend[bend]; ok {
		bend = "" // 選択中でなければ ↙ から曲げても普通の ↙
	}
	if side == "R" && d.Sub != "en" {
		if f, ok := dirBendFn[bend]; ok {
			d.commonLong(f)
			return
		}
	}
	switch d.Sub {
	case "ja":
		if side == "L" {
			d.jaLeft(dir)
		} else {
			d.jaRight(dir, bend)
		}
	case "num":
		d.numFlick(side, dir, bend)
	case "cur":
		d.curFlick(side, dir, rep, bend)
	}
}

// switchTo は切り替え口（右 ↘）の行き先: 曲げなし=戻る先, ←=英字, ↑=矢印
func (d *Dir) switchTo(plain, bend string) {
	to := plain
	switch bend {
	case "bend":
		to = "en"
	case "bendup":
		to = "cur"
	}
	if to == d.Sub {
		to = "ja"
	}
	d.setSub(to, true)
}

// ----- 矢印 -----

var (
	curRight = map[int]struct {
		vk  uint16
		lab string
	}{dUp: {0x26, "↑"}, dDown: {0x28, "↓"}, dLeft: {0x25, "←"}, dRight: {0x27, "→"}}
	curLeft = map[int]struct {
		vk  uint16
		lab string
	}{dLeft: {0x24, "Home"}, dRight: {0x23, "End"}, dUp: {0x21, "PgUp"}, dDown: {0x22, "PgDn"}}
)

func (d *Dir) curFlick(side string, dir int, rep bool, bend string) {
	if side == "R" && dir == dDR {
		d.switchTo("ja", bend)
		return
	}
	m := curRight
	if side == "L" {
		m = curLeft
	}
	k, ok := m[dir]
	if !ok {
		d.warn()
		return
	}
	// 修飾キーは次の1回に付く。押さえ続けている間（rep）は同じ修飾キーのまま
	if !rep || !d.repSet {
		d.repMods = d.modVKs(false)
		d.repLab = d.modLabel()
		d.repSet = true
		if len(d.mods) > 0 {
			d.mods = nil
			d.notify()
		}
	}
	d.out.key(d.repMods, k.vk)
	d.out.snd(SndFunc, map[int]int{dUp: 9, dDown: 3, dLeft: 5, dRight: 7}[dir])
	if !rep {
		d.out.echo(d.repLab+k.lab, false)
	}
}

// 両方の親指で同時にトン（英字 → 日本語。修飾キーを付けている時は取り消し）
func (d *Dir) TwoTap() {
	if len(d.mods) > 0 && !d.selecting {
		d.mods = nil
		d.out.snd(SndFunc, 1)
		d.out.echo("修飾キー 取消", false)
		d.notify()
		return
	}
	if d.Sub == "en" {
		d.setSub("ja", true)
	}
}

func (d *Dir) commonLong(dir int) bool {
	switch dir {
	case dLeft:
		d.out.key(nil, vkBack)
		d.out.snd(SndHeavy, 2)
		d.out.echo("削除", false)
		if d.comp > 0 {
			d.comp--
		}
		d.last, d.row = 0, -1
		if len(d.word) > 0 {
			d.word = d.word[:len(d.word)-1]
		}
		return true
	case dDown:
		d.out.key(nil, vkReturn)
		d.out.snd(SndHeavy, 8)
		d.out.echo("確定", false)
		d.comp, d.conv, d.last, d.row, d.word = 0, false, 0, -1, nil
		return true
	case dRight:
		d.out.key(nil, vkTab)
		d.out.snd(SndHeavy, 11)
		d.out.echo("Tab", false)
		d.last, d.row, d.word = 0, -1, nil
		return true
	}
	return false
}

// ----- 日本語 -----

func (d *Dir) jaLeft(dir int) {
	if d.conv {
		switch dir {
		case dLeft, dRight: // 区切り（文節）を動かす
			vk := uint16(0x25)
			if dir == dRight {
				vk = 0x27
			}
			d.out.key([]uint16{modVK["shift"]}, vk)
			d.out.snd(SndFunc, map[int]int{dLeft: 3, dRight: 9}[dir])
			return
		}
		// 新しく打ち始めると、変換中の文字は日本語入力が自動で確定する
		d.conv, d.comp = false, 0
	}
	d.row = dirRowOf[dir]
	d.out.snd(SndLetter, d.row+2)
	d.out.echo(string(dirRowName[d.row])+"行", false)
}

func (d *Dir) jaRight(dir int, bend string) {
	if dir == dDR { // 切り替え口
		d.switchTo("num", bend)
		return
	}
	if d.conv && d.row < 0 {
		switch dir {
		case dDown, dUR: // 次の候補
			d.out.key(nil, vkSpace)
			d.out.snd(SndFunc, 5)
			return
		case dUp: // 前の候補
			d.out.key(nil, 0x26)
			d.out.snd(SndFunc, 8)
			return
		case dRight: // 確定
			d.out.key(nil, vkReturn)
			d.out.snd(SndHeavy, 8)
			d.out.echo("確定", false)
			d.conv, d.comp, d.last = false, 0, 0
			return
		case dLeft: // 取り消し（ひらがなに戻す）
			d.out.key(nil, vkEscape)
			d.out.snd(SndFunc, 2)
			d.out.echo("取消", false)
			d.conv = false
			return
		}
		// それ以外を打つと、変換中の文字は自動で確定する
		d.conv, d.comp = false, 0
	}
	if d.row >= 0 {
		row := d.row
		d.row = -1
		switch dir {
		case dUL, dDL: // 記号
			s := dirSyms[row][map[int]int{dUL: 0, dDL: 1}[dir]]
			if s.label == "" {
				d.warn()
				return
			}
			d.symbol(s)
			return
		case dUR: // 行を選んだまま変換 → 行は取り消して変換
			d.convert()
			return
		}
		d.kana(row, dirVowelOf[dir])
		return
	}
	switch dir {
	case dUL:
		d.out.typeASCII("nn")
		d.typed('ん', 5)
	case dDL:
		d.dakuten()
	case dUR:
		d.convert()
	default:
		v := dirVowelOf[dir]
		d.out.typeASCII(string(dirVowels[v]))
		d.typed([]rune("あいうえお")[v], v)
	}
}

func (d *Dir) typed(k rune, v int) {
	d.comp++
	d.last = k
	d.out.snd(SndVowel, v*2+2)
	d.out.echo(string(k), false)
}

func (d *Dir) kana(row, v int) {
	k := []rune(dirKana[row])[v]
	switch {
	case row == 6 && v == 1: // や行の い → ー
		d.out.typeASCII("-")
		d.comp++
		d.last = 0
		d.out.snd(SndSymbol, 6)
		d.out.echo("ー", false)
	case row == 6 && v == 3: // や行の え → 空白（入力途中なら確定してから）
		if d.comp > 0 {
			d.out.key(nil, vkReturn)
		}
		d.out.key(nil, vkSpace)
		d.comp, d.last = 0, 0
		d.out.snd(SndHeavy, 6)
		d.out.echo("空白", false)
	case row == 8 && v >= 1 && v <= 3: // わ行の い・う・え → 、。・
		d.out.typeASCII([]string{",", ".", "/"}[v-1])
		d.comp++
		d.last = 0
		d.out.snd(SndSymbol, 6)
		d.out.echo([]string{"、", "。", "・"}[v-1], false)
	default:
		d.out.typeASCII(dirRoma[k])
		d.typed(k, v)
	}
}

func (d *Dir) symbol(s dirSym) {
	if s.ascii != "" {
		d.out.typeASCII(s.ascii)
		d.comp++
	} else {
		// 日本語入力を通さずに直接入れる記号（入力途中の文字は確定されることがある）
		d.out.uni(s.uni)
		d.comp = 0
	}
	d.last = 0
	d.out.snd(SndSymbol, 6)
	d.out.echo(s.label, false)
}

func (d *Dir) dakuten() {
	for _, c := range dirCycles {
		rs := []rune(c)
		for i, r := range rs {
			if r == d.last {
				n := rs[(i+1)%len(rs)]
				d.out.key(nil, vkBack)
				d.out.typeASCII(dirRoma[n])
				d.last = n
				d.out.snd(SndVowel, 10)
				d.out.echo(string(n), false)
				return
			}
		}
	}
	d.warn()
}

func (d *Dir) convert() {
	if d.comp <= 0 {
		d.warn()
		return
	}
	d.out.key(nil, vkSpace)
	if !d.conv {
		d.conv = true
		d.out.snd(SndMod, 9)
		d.out.echo("変換", false)
	} else {
		d.out.snd(SndFunc, 5)
	}
}

// ----- 数字 -----

func (d *Dir) numFlick(side string, dir int, bend string) {
	if side == "R" && dir == dDR {
		d.switchTo("ja", bend)
		return
	}
	var s string
	if side == "L" {
		s = dirNumL[dir]
	} else {
		s = dirNumR[dir]
	}
	if s == "" {
		d.warn()
		return
	}
	d.out.typeASCII(s)
	if s[0] >= '0' && s[0] <= '9' {
		d.out.snd(SndLetter, int(s[0]-'0'))
	} else {
		d.out.snd(SndSymbol, 6)
	}
	d.out.echo(s, false)
}

// ----- 英字（一筆書き） -----

func (d *Dir) Stroke(raw []pt, upper bool) {
	if d.inputKind() != "stroke" || d.rec == nil {
		return
	}
	ms := d.rec.Recognize(raw)
	if len(ms) == 0 || ms[0].Dist > d.rec.Threshold {
		d.warn()
		d.out.echo("?", true)
		return
	}
	best := ms[0]
	// 形が迷う時（2番目との差がわずか）は、英単語としてありそうな方を選ぶ
	if len(ms) > 1 && ms[1].Dist-best.Dist < 0.03 && len(ms[1].Key) == 1 && len(best.Key) == 1 {
		pre := strings.ToLower(string(d.word))
		a, b := englishPrior(pre, unicode.ToLower(rune(best.Key[0]))), englishPrior(pre, unicode.ToLower(rune(ms[1].Key[0])))
		if b > a*1.5 {
			best = ms[1]
		}
	}
	if len(d.mods) > 0 { // 修飾キー付きの1文字
		switch best.Key {
		case " ":
			d.useMods(vkSpace, "Space", upper)
		case "BS":
			d.useMods(vkBack, "BS", upper)
		case "ENTER":
			d.useMods(vkReturn, "Enter", upper)
		default:
			d.useMods(uint16(best.Key[0]), best.Key, upper)
		}
		return
	}
	switch best.Key {
	case " ":
		d.out.key(nil, vkSpace)
		d.word = nil
		d.out.snd(SndHeavy, 6)
		d.out.echo("空白", false)
	case "BS":
		d.commonLong(dLeft)
	case "ENTER":
		d.commonLong(dDown)
	default:
		c := best.Key
		if !upper {
			c = strings.ToLower(c)
		}
		d.out.typeASCII(c)
		d.word = append(d.word, []rune(c)...)
		d.out.snd(SndLetter, int(unicode.ToUpper(rune(c[0]))-'A')*12/26)
		d.out.echo(c, false)
	}
}
