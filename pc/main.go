package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// takeOver は動いている古いPhoneBoardを終了させ、このプロセスが代わりに動けるようにする。
func takeOver() bool {
	// 新しい版（v2.2.1以降）は設定画面のAPIで終了を頼める
	asked := false
	cl := &http.Client{Timeout: 800 * time.Millisecond}
	for p := 39501; p < 39511; p++ {
		req, _ := http.NewRequest("POST", "http://127.0.0.1:"+itoa(p)+"/api/quit", strings.NewReader("{}"))
		req.Header.Set("X-PhoneBoard", "1")
		if resp, err := cl.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				asked = true
				break
			}
		}
	}
	for i := 0; i < 30; i++ {
		if i == 10 || (!asked && i == 0) {
			killOtherInstances() // 古い版は終了の頼み方を知らないので、プロセスを終了させる
		}
		time.Sleep(100 * time.Millisecond)
		if acquireSingleInstance() {
			time.Sleep(300 * time.Millisecond) // ポートが空くのを少し待つ
			return true
		}
	}
	return false
}

func main() {
	logf, err := os.OpenFile(filepath.Join(dataDir(), "log.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err == nil {
		log.SetOutput(io.MultiWriter(logf, os.Stderr))
	}
	log.Printf("PhoneBoard PC %s", Version)

	if !acquireSingleInstance() {
		// 既に起動中なら、古い方を終了させて入れ替わる（新しいexeに差し替えた時に古い版が動き続けないように）
		log.Printf("another instance is running; taking over")
		if !takeOver() {
			openBrowser("http://127.0.0.1:39501/")
			return
		}
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
