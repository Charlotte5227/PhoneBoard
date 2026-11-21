//go:build windows

package main

import (
	"sync"
	"unsafe"
)

var (
	winmm       = syscallLazy("winmm.dll")
	pPlaySoundW = winmm.NewProc("PlaySoundW")
)

const (
	sndAsync     = 0x0001
	sndNoDefault = 0x0002
	sndMemory    = 0x0004
)

// winClicker は PlaySound で非同期にクリック音を鳴らす（音声はメモリ上に保持）。
type winClicker struct {
	mu    sync.Mutex
	cache map[[2]int][]byte
}

func NewClicker() Clicker { return &winClicker{cache: map[[2]int][]byte{}} }

func (c *winClicker) Click(big bool, volume int) {
	c.mu.Lock()
	k := [2]int{0, volume}
	if big {
		k[0] = 1
	}
	wav, ok := c.cache[k]
	if !ok {
		wav = clickWAV(big, volume)
		c.cache[k] = wav // 再生中に解放されないよう保持し続ける
	}
	c.mu.Unlock()
	pPlaySoundW.Call(uintptr(unsafe.Pointer(&wav[0])), 0, sndAsync|sndNoDefault|sndMemory)
}
