//go:build !windows

package main

import "log"

func NewInjector() Injector { return &LogInjector{} }

type nullClicker struct{}

func (nullClicker) Play(group string, semi int, volume int) {
	log.Printf("[sound] %s semi=%d vol=%d", group, semi, volume)
}

func NewClicker() Clicker { return nullClicker{} }

func acquireSingleInstance() bool { return true }
func initPlatform()               {}
func openBrowser(url string)      { log.Printf("open %s", url) }
func RunUI(app *App)              { select {} }
