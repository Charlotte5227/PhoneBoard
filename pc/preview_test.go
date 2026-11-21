package main

import (
	"encoding/binary"
	"os"
	"testing"
)

// PHONEBOARD_PREVIEW=ファイル名 を付けて実行すると、全種類の音を並べた試聴用WAVを書き出す。
func TestWritePreview(t *testing.T) {
	out := os.Getenv("PHONEBOARD_PREVIEW")
	if out == "" {
		t.Skip()
	}
	m := NewMixer()
	var pcm []int16
	gap := func(sec float64) {
		buf := make([]int16, int(sec*sampleRate))
		m.Fill(buf)
		pcm = append(pcm, buf...)
	}
	for _, g := range []string{SndVowel, SndLetter, SndSymbol, SndHeavy, SndFunc, SndMod} {
		for _, s := range []int{0, 3, 6, 9, 12} {
			m.Add(g, s, 80)
			gap(0.16)
		}
		gap(0.5)
	}
	// 速く打った時の重なり（k o n n i t i w a）
	for _, x := range []struct {
		g string
		s int
	}{{SndLetter, 7}, {SndVowel, 8}, {SndLetter, 6}, {SndLetter, 6}, {SndVowel, 7}, {SndLetter, 4}, {SndVowel, 7}, {SndLetter, 2}, {SndVowel, 2}} {
		m.Add(x.g, x.s, 80)
		gap(0.07)
	}
	gap(0.5)
	m.Add(SndWarn, 0, 80)
	gap(0.8)
	b := make([]byte, 44+len(pcm)*2)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+len(pcm)*2))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], sampleRate)
	binary.LittleEndian.PutUint32(b[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(len(pcm)*2))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(v))
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
