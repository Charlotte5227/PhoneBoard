package main

import (
	"fmt"
	"log"
	"sync"
)

// LogInjector は Windows 以外（開発・テスト用）で入力をログに出すだけの実装。
type LogInjector struct {
	mu  sync.Mutex
	Ops []string
	IME bool
}

func (l *LogInjector) rec(s string) {
	l.mu.Lock()
	l.Ops = append(l.Ops, s)
	l.mu.Unlock()
	log.Printf("[inject] %s", s)
}
func (l *LogInjector) KeyCombo(mods []uint16, vk uint16) { l.rec(fmt.Sprintf("key %v+%#x", mods, vk)) }
func (l *LogInjector) KeyDown(vk uint16)                 { l.rec(fmt.Sprintf("down %#x", vk)) }
func (l *LogInjector) KeyUp(vk uint16)                   { l.rec(fmt.Sprintf("up %#x", vk)) }
func (l *LogInjector) TypeASCII(s string)                { l.rec("ascii " + s) }
func (l *LogInjector) TypeUnicode(r rune)                { l.rec("uni " + string(r)) }
func (l *LogInjector) MouseMove(dx, dy int)              { l.rec(fmt.Sprintf("move %d,%d", dx, dy)) }
func (l *LogInjector) MouseButton(b int, down bool)      { l.rec(fmt.Sprintf("btn %d %v", b, down)) }
func (l *LogInjector) Wheel(d int, h bool)               { l.rec(fmt.Sprintf("wheel %d h=%v", d, h)) }
func (l *LogInjector) Foreground() uintptr               { return 0 }
func (l *LogInjector) SetIME(open, kana bool)            { l.rec(fmt.Sprintf("ime %v", open)) }
func (l *LogInjector) IMEOpen() bool                     { return l.IME }
