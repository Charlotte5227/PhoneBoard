//go:build !windows

package main

import "log"

func NewInjector() Injector { return &LogInjector{} }

type nullClicker struct{}

func (nullClicker) Click(big bool, volume int) { log.Printf("[click] big=%v vol=%d", big, volume) }

func NewClicker() Clicker { return nullClicker{} }

func acquireSingleInstance() bool { return true }
func initPlatform()               {}
func openBrowser(url string)      { log.Printf("open %s", url) }
func RunUI(app *App)              { select {} }
