//go:build !windows

package main

import "log"

func NewInjector() Injector { return &LogInjector{} }

func acquireSingleInstance() bool { return true }
func initPlatform()               {}
func openBrowser(url string)      { log.Printf("open %s", url) }
func RunUI(app *App)              { select {} }
