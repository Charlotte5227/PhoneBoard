//go:build windows

package main

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"
)

var (
	winmm                 = syscallLazy("winmm.dll")
	pPlaySoundW           = winmm.NewProc("PlaySoundW")
	pWaveOutOpen          = winmm.NewProc("waveOutOpen")
	pWaveOutPrepareHeader = winmm.NewProc("waveOutPrepareHeader")
	pWaveOutWrite         = winmm.NewProc("waveOutWrite")
	pCreateEventW         = kernel32.NewProc("CreateEventW")
	pWaitForSingleObject  = kernel32.NewProc("WaitForSingleObject")
)

const (
	sndAsync      = 0x0001
	sndNoDefault  = 0x0002
	sndMemory     = 0x0004
	waveMapper    = 0xFFFFFFFF
	callbackEvent = 0x00050000
	whdrDone      = 0x00000001
	audioFrames   = 441 // 10ms
	audioBuffers  = 4   // 先読みは約40ms（＝音が鳴るまでの遅れの上限）
)

type waveFormatEx struct {
	FormatTag      uint16
	Channels       uint16
	SamplesPerSec  uint32
	AvgBytesPerSec uint32
	BlockAlign     uint16
	BitsPerSample  uint16
	CbSize         uint16
}

type waveHdr struct {
	Data          uintptr
	BufferLength  uint32
	BytesRecorded uint32
	User          uintptr
	Flags         uint32
	Loops         uint32
	Next          uintptr
	Reserved      uintptr
}

// winClicker は waveOut で常に小さな音声バッファを流し続け、そこに音を混ぜて鳴らす。
// 速く打っても音が重なって鳴り、前の音が途切れない。使えない環境では PlaySound で鳴らす。
type winClicker struct {
	mix  *Mixer
	ok   atomic.Bool
	mu   sync.Mutex
	wavs map[[3]int][]byte
}

func NewClicker() Clicker {
	c := &winClicker{mix: NewMixer(), wavs: map[[3]int][]byte{}}
	ready := make(chan bool, 1)
	go c.run(ready)
	select {
	case ok := <-ready:
		c.ok.Store(ok)
	case <-time.After(2 * time.Second):
	}
	log.Printf("audio: low-latency mixer=%v", c.ok.Load())
	return c
}

func (c *winClicker) run(ready chan<- bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("audio mixer stopped: %v", r)
			c.ok.Store(false)
			select {
			case ready <- false:
			default:
			}
		}
	}()
	runtime.LockOSThread()
	ev, _, _ := pCreateEventW.Call(0, 0, 0, 0)
	if ev == 0 {
		ready <- false
		return
	}
	wfx := waveFormatEx{FormatTag: 1, Channels: 1, SamplesPerSec: sampleRate, AvgBytesPerSec: sampleRate * 2, BlockAlign: 2, BitsPerSample: 16}
	var hwo uintptr
	if r, _, _ := pWaveOutOpen.Call(uintptr(unsafe.Pointer(&hwo)), waveMapper, uintptr(unsafe.Pointer(&wfx)), ev, 0, callbackEvent); r != 0 {
		log.Printf("waveOutOpen failed: %d", r)
		ready <- false
		return
	}
	bufs := make([][]int16, audioBuffers)
	hdrs := make([]*waveHdr, audioBuffers)
	hsz := unsafe.Sizeof(waveHdr{})
	for i := range bufs {
		bufs[i] = make([]int16, audioFrames)
		hdrs[i] = &waveHdr{Data: uintptr(unsafe.Pointer(&bufs[i][0])), BufferLength: audioFrames * 2}
		if r, _, _ := pWaveOutPrepareHeader.Call(hwo, uintptr(unsafe.Pointer(hdrs[i])), hsz); r != 0 {
			ready <- false
			return
		}
		pWaveOutWrite.Call(hwo, uintptr(unsafe.Pointer(hdrs[i])), hsz)
	}
	ready <- true
	for {
		pWaitForSingleObject.Call(ev, 100)
		for i, h := range hdrs {
			if atomic.LoadUint32(&h.Flags)&whdrDone == 0 {
				continue
			}
			c.mix.Fill(bufs[i])
			atomic.StoreUint32(&h.Flags, atomic.LoadUint32(&h.Flags)&^whdrDone)
			pWaveOutWrite.Call(hwo, uintptr(unsafe.Pointer(h)), hsz)
		}
	}
}

func (c *winClicker) Play(group string, semi int, volume int) {
	if c.ok.Load() {
		c.mix.Add(group, semi, volume)
		return
	}
	// 予備: PlaySound（同時には1音しか鳴らない）
	c.mu.Lock()
	k := [3]int{indexOf(group), semi, volume}
	wav, ok := c.wavs[k]
	if !ok {
		wav = renderWAV(group, semi, volume)
		c.wavs[k] = wav // 再生中に解放されないよう保持し続ける
	}
	c.mu.Unlock()
	pPlaySoundW.Call(uintptr(unsafe.Pointer(&wav[0])), 0, sndAsync|sndNoDefault|sndMemory)
}

func indexOf(g string) int {
	for i, s := range SoundGroups {
		if s == g {
			return i
		}
	}
	return 0
}
