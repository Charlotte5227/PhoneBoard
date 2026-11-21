package main

import (
	"encoding/binary"
	"math"
	"math/rand"
	"sync"
)

// キー音の系統（音色）。音の高さはキーの横位置で変える（左が低く、右が高い）。
const (
	SndVowel  = "vowel"  // 母音 a i u e o: 明るい「ピッ」（ローマ字で1音節が完成した合図）
	SndLetter = "letter" // 子音などの英字: 普通の打鍵音
	SndSymbol = "symbol" // 数字・記号: 木のような音
	SndHeavy  = "heavy"  // Space / Enter / BS: 低く重い音
	SndFunc   = "func"   // Esc・Tab・矢印・F1〜F12 など: 二重の「カチカチ」
	SndMod    = "mod"    // Shift・Ctrl など: 小さな「カチッ」
	SndWarn   = "warn"   // 注意音: 下がり調子の2音「ポッポッ」
)

var SoundGroups = []string{SndVowel, SndLetter, SndSymbol, SndHeavy, SndFunc, SndMod, SndWarn}

const sampleRate = 44100

// Clicker は音を鳴らす（Windows: sound_windows.go）。
type Clicker interface {
	Play(group string, semi int, volume int)
}

// synth は系統ごとの音（-1〜1、モノラル44.1kHz）を合成する。
func synth(group string) []float32 {
	rng := rand.New(rand.NewSource(int64(len(group)) + 7))
	gen := func(dur float64, f func(t float64) float64) []float32 {
		n := int(dur * sampleRate)
		out := make([]float32, n)
		for i := range out {
			out[i] = float32(f(float64(i) / sampleRate))
		}
		return out
	}
	noise := func() float64 { return rng.Float64()*2 - 1 }
	sin := func(f, t float64) float64 { return math.Sin(2 * math.Pi * f * t) }
	switch group {
	case SndVowel:
		return gen(0.055, func(t float64) float64 {
			return 0.65*(sin(1900, t)+0.3*sin(3800, t))*math.Exp(-t/0.022) + 0.25*noise()*math.Exp(-t/0.003)
		})
	case SndSymbol:
		return gen(0.040, func(t float64) float64 {
			tri := 2/math.Pi*math.Asin(sin(950, t)) + 0.4*sin(1900, t)
			return 0.6*tri*math.Exp(-t/0.012) + 0.35*noise()*math.Exp(-t/0.004)
		})
	case SndHeavy:
		return gen(0.070, func(t float64) float64 {
			return 0.8*sin(430, t)*math.Exp(-t/0.028) + 0.45*noise()*math.Exp(-t/0.007)
		})
	case SndFunc:
		return gen(0.045, func(t float64) float64 {
			v := 0.5 * sin(1700, t) * math.Exp(-t/0.006)
			if t2 := t - 0.018; t2 >= 0 {
				v += 0.5 * sin(1700, t2) * math.Exp(-t2/0.006)
			}
			return v + 0.3*noise()*math.Exp(-t/0.003)
		})
	case SndMod:
		return gen(0.022, func(t float64) float64 {
			return 0.45*sin(3000, t)*math.Exp(-t/0.004) + 0.2*noise()*math.Exp(-t/0.002)
		})
	case SndWarn:
		return gen(0.200, func(t float64) float64 {
			tone := func(f, t0, d float64) float64 {
				x := t - t0
				if x < 0 || x > d {
					return 0
				}
				env := math.Min(1, x/0.004) * math.Exp(-x/(d*0.6))
				return (sin(f, x) + 0.25*sin(3*f, x)) * env
			}
			return 0.75 * (tone(1050, 0, 0.065) + tone(700, 0.090, 0.100))
		})
	default: // SndLetter
		return gen(0.032, func(t float64) float64 {
			return 0.45*sin(1500, t)*math.Exp(-t/0.010) + 0.55*noise()*math.Exp(-t/0.006)
		})
	}
}

// semiRate は半音 semi の再生速度（音の高さ）。
func semiRate(group string, semi int) float64 {
	if group == SndWarn {
		return 1
	}
	return math.Pow(2, float64(semi)/12)
}

// voice は再生中の音1つ。
type voice struct {
	s    []float32
	pos  float64
	rate float64
	vol  float64
}

// Mixer は複数の音を重ねて鳴らす（速く打っても前の音が途切れない）。
type Mixer struct {
	mu     sync.Mutex
	voices []*voice
	cache  map[string][]float32
}

func NewMixer() *Mixer { return &Mixer{cache: map[string][]float32{}} }

func (m *Mixer) Add(group string, semi int, volume int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.cache[group]
	if !ok {
		s = synth(group)
		m.cache[group] = s
	}
	if len(m.voices) > 24 {
		m.voices = m.voices[1:]
	}
	m.voices = append(m.voices, &voice{s: s, rate: semiRate(group, semi), vol: float64(volume) / 100})
}

func (m *Mixer) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.voices) > 0
}

// Fill は out を混ぜ合わせた音で埋める（16bitモノラル）。
func (m *Mixer) Fill(out []int16) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range out {
		var acc float64
		for _, v := range m.voices {
			p := int(v.pos)
			if p+1 < len(v.s) {
				fr := v.pos - float64(p)
				acc += (float64(v.s[p])*(1-fr) + float64(v.s[p+1])*fr) * v.vol
			}
			v.pos += v.rate
		}
		// 重なって大きくなりすぎた時はやわらかく抑える
		acc = math.Tanh(acc*0.9) / 0.9
		out[i] = int16(clampF(acc, -1, 1) * 32000)
	}
	live := m.voices[:0]
	for _, v := range m.voices {
		if int(v.pos)+1 < len(v.s) {
			live = append(live, v)
		}
	}
	m.voices = live
}

// renderWAV は1つの音をWAVにする（低遅延の出力が使えない時の予備）。
func renderWAV(group string, semi, volume int) []byte {
	s := synth(group)
	rate := semiRate(group, semi)
	n := int(float64(len(s)) / rate)
	pcm := make([]int16, n)
	vol := float64(volume) / 100
	for i := range pcm {
		pos := float64(i) * rate
		p := int(pos)
		if p+1 >= len(s) {
			break
		}
		fr := pos - float64(p)
		pcm[i] = int16(clampF((float64(s[p])*(1-fr)+float64(s[p+1])*fr)*vol, -1, 1) * 32000)
	}
	b := make([]byte, 44+n*2)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n*2))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], sampleRate)
	binary.LittleEndian.PutUint32(b[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n*2))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(v))
	}
	return b
}
