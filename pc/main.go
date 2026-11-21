package main

import (
	"io"
	"log"
	"os"
	"path/filepath"
)

func main() {
	logf, err := os.OpenFile(filepath.Join(dataDir(), "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err == nil {
		log.SetOutput(io.MultiWriter(logf, os.Stderr))
	}
	log.Printf("PhoneBoard PC %s", Version)

	if !acquireSingleInstance() {
		// 既に起動中なら設定画面だけ開いて終了
		openBrowser("http://127.0.0.1:39501/")
		return
	}
	initPlatform()

	cfg := LoadConfig()
	lay := LoadLayouts()
	app := NewApp(cfg, lay, NewInjector())
	app.OpenURL = openBrowser

	go app.TickLoop()
	go RunServer(app, cfg.Port)
	go RunUDP(app, cfg.Port)
	ready := make(chan string, 1)
	go RunWeb(app, 39501, ready)
	url := <-ready
	if cfg.OpenOnStart && url != "" {
		openBrowser(url)
	}
	RunUI(app) // Windows: オーバーレイとタスクトレイのメッセージループ
}
