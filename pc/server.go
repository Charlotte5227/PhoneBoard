package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

const Version = "2.5.0"

// Session は1台のスマホとのTCP接続。改行区切りJSONでやり取りする。
type Session struct {
	conn   net.Conn
	out    chan []byte
	once   sync.Once
	closed chan struct{}
	name   string
}

func (s *Session) Name() string { return s.name }

func (s *Session) Send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	b = append(b, '\n')
	select {
	case s.out <- b:
	case <-s.closed:
	default: // 詰まっている時は捨てる（振動など）
	}
}

func (s *Session) Close() {
	s.once.Do(func() {
		close(s.closed)
		_ = s.conn.Close()
	})
}

func (s *Session) writer() {
	for {
		select {
		case b := <-s.out:
			_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := s.conn.Write(b); err != nil {
				s.Close()
				return
			}
		case <-s.closed:
			return
		}
	}
}

func RunServer(app *App, port int) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("TCP listen error: %v", err)
		app.toast(fmt.Sprintf("ポート%dを開けません（多重起動？）", port))
		return
	}
	log.Printf("listening tcp :%d", port)
	for {
		c, err := ln.Accept()
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go handleConn(app, c)
	}
}

func handleConn(app *App, c net.Conn) {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(5 * time.Second)
	}
	s := &Session{conn: c, out: make(chan []byte, 512), closed: make(chan struct{})}
	go s.writer()
	defer s.Close()

	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if !sc.Scan() {
		return
	}
	var hello Msg
	if err := json.Unmarshal(sc.Bytes(), &hello); err != nil || hello.T != "hello" {
		return
	}
	cfg := app.Config()
	if cfg.RequirePin && strings.TrimSpace(hello.Pin) != cfg.PIN {
		s.Send(map[string]any{"t": "denied", "reason": "pin"})
		log.Printf("PIN mismatch from %s", c.RemoteAddr())
		app.toast("PINが違うため接続を拒否しました")
		time.Sleep(300 * time.Millisecond)
		return
	}
	host, _ := os.Hostname()
	s.name = hello.Name
	tok := randomToken()
	// udp: カーソル移動などをUDPで送ってよいポートと、その合言葉
	s.Send(map[string]any{"t": "welcome", "host": host, "v": Version, "udp": cfg.Port, "tok": tok})
	app.OnConnect(s, PhoneInfo{Name: hello.Name, Wdp: hello.Wdp, Hdp: hello.Hdp}, tok)
	defer app.OnDisconnect(s)

	for {
		_ = c.SetReadDeadline(time.Now().Add(20 * time.Second))
		if !sc.Scan() {
			return
		}
		var m Msg
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			continue
		}
		app.Handle(s, &m)
	}
}

// RunUDP はスマホからの「PHONEBOARD?」(PCの自動検出)に応答し、
// UDPで届くマウスの動き（JSON、合言葉つき）を処理する。
func RunUDP(app *App, port int) {
	pc, err := net.ListenPacket("udp4", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Printf("UDP listen error: %v", err)
		return
	}
	host, _ := os.Hostname()
	buf := make([]byte, 2048)
	for {
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			continue
		}
		b := buf[:n]
		if len(b) > 0 && b[0] == '{' {
			var m Msg
			if json.Unmarshal(b, &m) == nil {
				app.HandleUDP(&m)
			}
			continue
		}
		if strings.HasPrefix(string(b), "PHONEBOARD?") {
			reply := fmt.Sprintf("PHONEBOARD!|%d|%s|%s", port, host, Version)
			_, _ = pc.WriteTo([]byte(reply), addr)
		}
	}
}

func randomToken() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func localIPs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}
