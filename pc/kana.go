package main

import "unicode"

// かな → ローマ字（IMEへキー入力として送る）
var romaji = map[rune]string{
	'あ': "a", 'い': "i", 'う': "u", 'え': "e", 'お': "o",
	'か': "ka", 'き': "ki", 'く': "ku", 'け': "ke", 'こ': "ko",
	'さ': "sa", 'し': "si", 'す': "su", 'せ': "se", 'そ': "so",
	'た': "ta", 'ち': "ti", 'つ': "tu", 'て': "te", 'と': "to",
	'な': "na", 'に': "ni", 'ぬ': "nu", 'ね': "ne", 'の': "no",
	'は': "ha", 'ひ': "hi", 'ふ': "hu", 'へ': "he", 'ほ': "ho",
	'ま': "ma", 'み': "mi", 'む': "mu", 'め': "me", 'も': "mo",
	'や': "ya", 'ゆ': "yu", 'よ': "yo",
	'ら': "ra", 'り': "ri", 'る': "ru", 'れ': "re", 'ろ': "ro",
	'わ': "wa", 'を': "wo", 'ん': "nn",
	'が': "ga", 'ぎ': "gi", 'ぐ': "gu", 'げ': "ge", 'ご': "go",
	'ざ': "za", 'じ': "zi", 'ず': "zu", 'ぜ': "ze", 'ぞ': "zo",
	'だ': "da", 'ぢ': "di", 'づ': "du", 'で': "de", 'ど': "do",
	'ば': "ba", 'び': "bi", 'ぶ': "bu", 'べ': "be", 'ぼ': "bo",
	'ぱ': "pa", 'ぴ': "pi", 'ぷ': "pu", 'ぺ': "pe", 'ぽ': "po",
	'ぁ': "la", 'ぃ': "li", 'ぅ': "lu", 'ぇ': "le", 'ぉ': "lo",
	'ゃ': "lya", 'ゅ': "lyu", 'ょ': "lyo", 'っ': "ltu", 'ゎ': "lwa",
	'ゔ': "vu",
	'ー': "-", '、': ",", '。': ".", '？': "?", '！': "!",
	'「': "[", '」': "]", '・': "/", '〜': "~", '～': "~",
}

// 濁点・半濁点・小文字の切替表
var dakuMap = map[rune]rune{}
var handakuMap = map[rune]rune{}
var smallMap = map[rune]rune{}
var baseOf = map[rune]rune{}

// ゛゜小 キーのタップで巡回する順序（iPhone準拠）
var cycles = map[rune][]rune{}

func init() {
	pairs := func(a, b string, m map[rune]rune) {
		ar, br := []rune(a), []rune(b)
		for i := range ar {
			m[ar[i]] = br[i]
			baseOf[br[i]] = ar[i]
		}
	}
	pairs("かきくけこさしすせそたちつてとはひふへほう", "がぎぐげござじずぜぞだぢづでどばびぶべぼゔ", dakuMap)
	pairs("はひふへほ", "ぱぴぷぺぽ", handakuMap)
	pairs("あいうえおやゆよつわ", "ぁぃぅぇぉゃゅょっゎ", smallMap)

	for b := range dakuMap {
		seq := []rune{b}
		if s, ok := smallMap[b]; ok {
			seq = append(seq, s)
		}
		seq = append(seq, dakuMap[b])
		if h, ok := handakuMap[b]; ok {
			seq = append(seq, h)
		}
		cycles[b] = seq
	}
	for b, s := range smallMap {
		if _, ok := cycles[b]; !ok {
			cycles[b] = []rune{b, s}
		}
	}
}

func kanaBase(r rune) rune {
	if b, ok := baseOf[r]; ok {
		return b
	}
	return r
}

// cycleRune は ゛゜小 キーのタップ（英字では大文字小文字切替）。
func cycleRune(r rune) rune {
	if unicode.IsLetter(r) && r < 128 {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}
	seq, ok := cycles[kanaBase(r)]
	if !ok {
		return r
	}
	for i, c := range seq {
		if c == r {
			return seq[(i+1)%len(seq)]
		}
	}
	return r
}

func toggleMap(r rune, m map[rune]rune) rune {
	b := kanaBase(r)
	v, ok := m[b]
	if !ok {
		return r
	}
	if r == v {
		return b
	}
	return v
}

func dakuRune(r rune) rune    { return toggleMap(r, dakuMap) }
func handakuRune(r rune) rune { return toggleMap(r, handakuMap) }
func smallRune(r rune) rune   { return toggleMap(r, smallMap) }
