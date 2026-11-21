package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

// RunWeb は設定画面（http://127.0.0.1:39501）を提供する。PC内からのみアクセス可能。
func RunWeb(app *App, port int, ready chan<- string) {
	var ln net.Listener
	var err error
	for p := port; p < port+10; p++ {
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			break
		}
	}
	if err != nil {
		log.Printf("web listen error: %v", err)
		ready <- ""
		return
	}
	url := "http://" + ln.Addr().String() + "/"
	app.mu.Lock()
	app.webURL = url
	app.mu.Unlock()
	ready <- url

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		b, _ := webFS.ReadFile("web/index.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, app.Status())
	})
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			c := DefaultConfig()
			if !readJSON(w, r, c) {
				return
			}
			if err := app.SetConfig(c); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		writeJSON(w, app.Config())
	})
	mux.HandleFunc("/api/config/default", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, DefaultConfig())
	})
	mux.HandleFunc("/api/layouts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			l := &Layouts{}
			if !readJSON(w, r, l) {
				return
			}
			if err := app.SetLayouts(l); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		writeJSON(w, app.Layouts())
	})
	mux.HandleFunc("/api/layouts/default", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, DefaultLayouts())
	})
	mux.HandleFunc("/api/pin/new", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		c := app.Config()
		c.PIN = randomPIN()
		_ = app.SetConfig(c)
		writeJSON(w, app.Status())
	})
	mux.HandleFunc("/api/preview", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		app.Preview()
		writeJSON(w, map[string]bool{"ok": true})
	})
	// 新しい版を起動した時に、古い版（このプロセス）に終了を頼むためのAPI
	mux.HandleFunc("/api/quit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		log.Printf("quit requested by a new instance")
		go func() {
			time.Sleep(100 * time.Millisecond)
			RequestQuit()
		}()
	})
	mux.HandleFunc("/api/calib/reset", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		app.ResetCalib()
		writeJSON(w, app.Status())
	})
	mux.HandleFunc("/api/testclick", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", 405)
			return
		}
		semi := -1
		if v := r.URL.Query().Get("p"); v != "" {
			fmt.Sscan(v, &semi)
		}
		app.TestSound(r.URL.Query().Get("g"), semi)
		writeJSON(w, map[string]bool{"ok": true})
	})

	srv := &http.Server{Handler: guard(mux)}
	log.Printf("settings: %s", url)
	_ = srv.Serve(ln)
}

// guard は他サイトからの書き換え（DNSリバインディング/CSRF）を防ぐ。
func guard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		if host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-PhoneBoard") != "1" {
			http.Error(w, "forbidden", 403)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		http.Error(w, "bad json: "+err.Error(), 400)
		return false
	}
	return true
}
