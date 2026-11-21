package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
)

// 日本語として不自然な「かなの並び」の検出。
// 打ったローマ字をかなに直し、1音（か・きゃ・っ・ん・ー など）がそろうたびに、
// 直前の1音からの続き方が日本語として極端に珍しいかを調べる。
// 統計は wordfreq の日本語の単語頻度を読みがなにして数えたもの（kana_ja.LICENSE.txt 参照）。

//go:embed kana_ja.txt.gz
var kanaGz []byte

var (
	kanaUni   = map[string]float64{} // 1音の出現数
	kanaBi    = map[[2]string]float64{}
	kanaFrom  = map[string]float64{} // その音の後に続く数の合計
	kanaTotal float64
)

func init() {
	r, err := gzip.NewReader(bytes.NewReader(kanaGz))
	if err != nil {
		return
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		switch {
		case len(f) == 3 && f[0] == "U":
			v, _ := strconv.ParseFloat(f[2], 64)
			kanaUni[f[1]] = v
			kanaTotal += v
		case len(f) == 4 && f[0] == "B":
			v, _ := strconv.ParseFloat(f[3], 64)
			kanaBi[[2]string{f[1], f[2]}] = v
			kanaFrom[f[1]] += v
		}
	}
}

// kanaNext は音 a の次に b が来る確率（なめらかにしたもの）。
func kanaNext(a, b string) float64 {
	const alpha = 20.0
	pu := (kanaUni[b] + 1) / (kanaTotal + 300)
	return (kanaBi[[2]string{a, b}] + alpha*pu) / (kanaFrom[a] + alpha)
}

// ローマ字 → かな（Google日本語入力・MS-IMEの一般的なローマ字表）
var romaTable = map[string]string{}
var romaPrefix = map[string]bool{}

func init() {
	add := func(r, k string) { romaTable[r] = k }
	rows := []struct{ c, k string }{
		{"", "あいうえお"}, {"k", "かきくけこ"}, {"s", "さしすせそ"}, {"t", "たちつてと"}, {"n", "なにぬねの"},
		{"h", "はひふへほ"}, {"m", "まみむめも"}, {"r", "らりるれろ"}, {"g", "がぎぐげご"}, {"z", "ざじずぜぞ"},
		{"d", "だぢづでど"}, {"b", "ばびぶべぼ"}, {"p", "ぱぴぷぺぽ"}, {"c", "かしくせこ"},
	}
	for _, rw := range rows {
		k := []rune(rw.k)
		for i, v := range "aiueo" {
			add(rw.c+string(v), string(k[i]))
		}
	}
	for r, k := range map[string]string{
		"ya": "や", "yu": "ゆ", "yo": "よ", "ye": "いぇ", "wa": "わ", "wi": "うぃ", "we": "うぇ", "wo": "を", "wu": "う",
		"shi": "し", "chi": "ち", "tsu": "つ", "fu": "ふ", "ji": "じ", "nn": "ん", "n'": "ん", "xn": "ん",
		"fa": "ふぁ", "fi": "ふぃ", "fe": "ふぇ", "fo": "ふぉ", "va": "ゔぁ", "vi": "ゔぃ", "vu": "ゔ", "ve": "ゔぇ", "vo": "ゔぉ",
		"tsa": "つぁ", "tsi": "つぃ", "tse": "つぇ", "tso": "つぉ", "thi": "てぃ", "thu": "てゅ", "dhi": "でぃ", "dhu": "でゅ",
		"twu": "とぅ", "dwu": "どぅ", "qa": "くぁ", "qi": "くぃ", "qe": "くぇ", "qo": "くぉ", "-": "ー",
		"ja": "じゃ", "ju": "じゅ", "je": "じぇ", "jo": "じょ", "sha": "しゃ", "shu": "しゅ", "she": "しぇ", "sho": "しょ",
		"cha": "ちゃ", "chu": "ちゅ", "che": "ちぇ", "cho": "ちょ",
		"xtu": "っ", "ltu": "っ", "xtsu": "っ", "ltsu": "っ", "xwa": "ゎ", "lwa": "ゎ",
	} {
		add(r, k)
	}
	smallY := map[string]string{"a": "ゃ", "u": "ゅ", "o": "ょ", "i": "ぃ", "e": "ぇ"}
	for c, i := range map[string]string{"k": "き", "s": "し", "t": "ち", "c": "ち", "n": "に", "h": "ひ", "m": "み", "r": "り",
		"g": "ぎ", "z": "じ", "j": "じ", "d": "ぢ", "b": "び", "p": "ぴ", "f": "ふ", "v": "ゔ"} {
		for v, s := range smallY {
			add(c+"y"+v, i+s)
		}
	}
	for v, s := range map[string]string{"a": "ぁ", "i": "ぃ", "u": "ぅ", "e": "ぇ", "o": "ぉ"} {
		add("x"+v, s)
		add("l"+v, s)
	}
	for v, s := range map[string]string{"a": "ゃ", "u": "ゅ", "o": "ょ"} {
		add("xy"+v, s)
		add("ly"+v, s)
	}
	for r := range romaTable {
		for i := 1; i < len(r); i++ {
			romaPrefix[r[:i]] = true
		}
	}
}

func isConsonantByte(c byte) bool {
	return c >= 'a' && c <= 'z' && !isVowel(rune(c))
}

// romaToMora はローマ字列を1音ずつのかなにする。最後のまだ決まっていない部分は pending で返す。
// 変換できない文字は "?" になる。
func romaToMora(s string) (moras []string, pending string) {
	i := 0
	for i < len(s) {
		// 長い順に表を探す
		matched := false
		for l := 4; l >= 1; l-- {
			if i+l > len(s) {
				continue
			}
			if k, ok := romaTable[s[i:i+l]]; ok {
				// "n" + 母音 以外の "n" は表に無いので下で扱う。"nn" は ん。
				moras = append(moras, k)
				i += l
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		rest := s[i:]
		switch {
		case rest[0] == 'n' && len(rest) >= 2 && rest[1] != 'y' && !isVowel(rune(rest[1])):
			moras = append(moras, "ん") // n + 子音 → ん
			i++
		case len(rest) >= 2 && rest[0] == rest[1] && isConsonantByte(rest[0]):
			moras = append(moras, "っ") // 子音の重ね → っ
			i++
		case romaPrefix[rest]:
			return moras, rest // まだ途中（k, ky, sh など）
		default:
			moras = append(moras, "?")
			i++
		}
	}
	return moras, ""
}

// composer は日本語入力中の「変換前の文字」を、IMEの画面表示と同じ形で再現する。
// （かな ＋ まだかなになっていないローマ字）。BS もIMEと同じく、
// 途中のローマ字があればその1文字、無ければかなを1文字消す。
// ※打ったキーの並びのままだと「かため→BS→な」が katamna になり、m の後に n が来たと誤判定してしまう。
type composer struct {
	kana []rune // かなになった部分（変換できない文字は '?'）
	pend string // まだかなになっていないローマ字（k, ky, sh, n など）
}

func (c *composer) reset() {
	c.kana = c.kana[:0]
	c.pend = ""
}

func (c *composer) empty() bool { return len(c.kana) == 0 && c.pend == "" }

func isSmallKana(r rune) bool { return strings.ContainsRune("ゃゅょぁぃぅぇぉゎ", r) }

// lastMora は最後の1音（きゃ などは2文字で1音）。
func lastMora(k []rune) string {
	n := len(k)
	if n == 0 {
		return ""
	}
	if n >= 2 && isSmallKana(k[n-1]) {
		return string(k[n-2:])
	}
	return string(k[n-1:])
}

// next は ch を打った時に新しくそろう音と、その後の途中のローマ字を返す（状態は変えない）。
func (c *composer) next(ch byte) ([]string, string) {
	return romaToMora(c.pend + string(ch))
}

func (c *composer) feed(ch byte) {
	moras, rest := c.next(ch)
	for _, m := range moras {
		c.kana = append(c.kana, []rune(m)...)
	}
	c.pend = rest
	if len(c.kana) > 200 {
		c.reset()
	}
}

// dash は「ー」。途中の n は「ん」にしてから伸ばす。
func (c *composer) dash() {
	if c.pend == "n" {
		c.kana = append(c.kana, 'ん')
	}
	c.pend = ""
	c.kana = append(c.kana, 'ー')
}

func (c *composer) backspace() {
	if c.pend != "" {
		c.pend = c.pend[:len(c.pend)-1]
		return
	}
	if n := len(c.kana); n > 0 {
		c.kana = c.kana[:n-1]
	}
}

// oddness は ch を打った時に新しくそろう音の続き方のうち、最も珍しいものの確率（無ければ 1）。
func (c *composer) oddness(ch byte) float64 {
	moras, _ := c.next(ch)
	prev := lastMora(c.kana)
	worst := 1.0
	for _, m := range moras {
		if m != "?" && prev != "" && !strings.ContainsRune(prev, '?') {
			if p := kanaNext(prev, m); p < worst {
				worst = p
			}
		}
		prev = m
	}
	return worst
}
