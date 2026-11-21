package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"sort"
	"strconv"
	"strings"
)

// 入力補助（初代iPhone風の「見えない当たり判定の拡大」）。
// 次に来そうな文字ほど、そのキーの当たり判定を少し広げる。
//   - 日本語入力中（IME ON）: ローマ字として成立する並びを優先（k の次は母音 など）
//   - 英字入力中（IME OFF）: 英単語の続きとして成立する文字を優先
// 入力した後で書き換えないので、変換や確定の邪魔にならない。

//go:embed words_en.txt.gz
var wordsGz []byte

// 英単語と出現頻度（wordfreq、words_en.LICENSE.txt 参照）。辞書順に並べ、頻度の累積和を持つ。
var (
	enWords []string
	enCum   []float64 // enCum[i] = enWords[:i] の頻度の合計
)

func init() {
	r, err := gzip.NewReader(bytes.NewReader(wordsGz))
	if err != nil {
		return
	}
	type wf struct {
		w string
		f float64
	}
	var list []wf
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), "\t", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		f, _ := strconv.ParseFloat(parts[1], 64)
		list = append(list, wf{parts[0], f})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].w < list[j].w })
	enWords = make([]string, len(list))
	enCum = make([]float64, len(list)+1)
	for i, x := range list {
		enWords[i] = x.w
		enCum[i+1] = enCum[i] + x.f
	}
}

// freqPrefix は prefix で始まる英単語の出現頻度の合計。
func freqPrefix(prefix string) float64 {
	if len(enWords) == 0 {
		return 0
	}
	lo := sort.SearchStrings(enWords, prefix)
	hi := sort.SearchStrings(enWords, prefix+"\x7f")
	return enCum[hi] - enCum[lo]
}

// englishPrior は単語の途中 prefix の次に ch が来るもっともらしさ（平均1程度）。
// よく使う単語ほど強く効く（"th" の次は e が r より圧倒的に多い、など）。
func englishPrior(prefix string, ch rune) float64 {
	if len(prefix) > 15 {
		return 1
	}
	n0 := freqPrefix(prefix)
	if n0 <= 0 {
		return 1 // 辞書に無い単語（略語・名前・パスワード等）には口出ししない
	}
	n1 := freqPrefix(prefix + string(ch))
	sm := n0 * 0.002 // ゼロにしないための下駄
	p := (n1 + sm) / (n0 + sm*26) * 26
	return clampF(p, 0.08, 6)
}

func isVowel(r rune) bool { return r == 'a' || r == 'i' || r == 'u' || r == 'e' || r == 'o' }

// romajiPending は直前の文字列のうち、まだかなになっていない子音部分を返す。
func romajiPending(ctx string) string {
	i := len(ctx)
	for i > 0 && !isVowel(rune(ctx[i-1])) {
		i--
	}
	p := ctx[i:]
	// "nn" は「ん」で確定
	for strings.HasPrefix(p, "nn") {
		p = p[2:]
	}
	// n + 子音（y以外）→ n は「ん」になる
	if len(p) >= 2 && p[0] == 'n' && p[1] != 'y' {
		p = p[1:]
	}
	// 促音 kk → k
	if len(p) >= 2 && p[0] == p[1] {
		p = p[1:]
	}
	if len(p) > 3 {
		p = p[len(p)-1:]
	}
	return p
}

// romajiPrior はローマ字入力として ch が次に来るもっともらしさ。
func romajiPrior(ctx string, ch rune) float64 {
	p := romajiPending(ctx)
	c := byte(ch)
	switch len(p) {
	case 0:
		// 音節のはじめ: 母音と一般的な子音
		if isVowel(ch) {
			return 1.3
		}
		if strings.IndexByte("kstnhmyrwgzdbpjfc", c) >= 0 {
			return 1.0
		}
		if strings.IndexByte("xlv", c) >= 0 {
			return 0.3
		}
		return 0.1 // q など
	case 1:
		k := p[0]
		if isVowel(ch) {
			switch {
			case k == 'y' && (c == 'i' || c == 'e'):
				return 0.3
			case k == 'w' && (c == 'i' || c == 'u' || c == 'e'):
				return 0.4
			}
			return 2.5
		}
		switch {
		case c == 'y' && strings.IndexByte("kgsztdnhbpmrcjfvxl", k) >= 0:
			return 1.0
		case c == 'h' && strings.IndexByte("sctdw", k) >= 0:
			return 1.3
		case k == 't' && c == 's':
			return 1.0
		case k == 'n' && c != 'y':
			return 1.0 // ん + 次の子音
		case c == k && strings.IndexByte("kstpcgzdbmhfjrw", k) >= 0:
			return 0.7 // 促音
		case (k == 'x' || k == 'l') && strings.IndexByte("ytwk", c) >= 0:
			return 1.0
		case k == 'd' && c == 'h', k == 'f' && c == 'y':
			return 0.8
		}
		return 0.08
	default:
		// ky / sh / ch / ts / ty など → 母音
		if isVowel(ch) {
			last := p[len(p)-1]
			if last == 'y' && (c == 'i' || c == 'e') {
				return 0.4
			}
			return 2.5
		}
		if p == "xt" || p == "lt" {
			if c == 's' {
				return 0.8
			}
		}
		return 0.06
	}
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Prior は文脈 ctx（英小文字の並び）の次に英字 ch が来るもっともらしさ。
func Prior(ctx string, ch rune, ime bool) float64 {
	if ime {
		return romajiPrior(ctx, ch)
	}
	return englishPrior(ctx, ch)
}
