package main

import (
	"encoding/binary"
	"math"
	"math/rand"
)

// clickWAV はキーのクリック音（短い打鍵音）を合成したWAVデータ。big は Space/Enter/BS 用の少し低い音。
func clickWAV(big bool, volume int) []byte {
	const rate = 44100
	dur := 0.028
	freq := 2400.0
	if big {
		dur, freq = 0.040, 1300.0
	}
	n := int(dur * rate)
	vol := float64(volume) / 100
	rng := rand.New(rand.NewSource(1))
	pcm := make([]int16, n)
	for i := range pcm {
		t := float64(i) / rate
		env := math.Exp(-t / 0.006)
		tone := math.Sin(2*math.Pi*freq*t) * math.Exp(-t/0.010)
		noise := (rng.Float64()*2 - 1) * env
		v := (0.55*noise + 0.45*tone) * vol * 0.9
		pcm[i] = int16(clampF(v, -1, 1) * 32767)
	}
	b := make([]byte, 44+n*2)
	copy(b[0:], "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n*2))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], rate)
	binary.LittleEndian.PutUint32(b[28:], rate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n*2))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(v))
	}
	return b
}
