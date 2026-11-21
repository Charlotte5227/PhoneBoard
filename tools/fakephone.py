#!/usr/bin/env python3
"""PCソフトの動作確認用の模擬スマホ（横向き・複数の指）。
使い方: python3 fakephone.py <PCのIP> <PIN>
"""
import json, socket, sys, time, threading

host = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
pin = sys.argv[2] if len(sys.argv) > 2 else ""

u = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
u.settimeout(1)
u.sendto(b"PHONEBOARD?", (host, 39500))
try:
    print("discovery:", u.recvfrom(256)[0].decode())
except socket.timeout:
    print("discovery: no reply")

s = socket.create_connection((host, 39500))
f = s.makefile("rw", encoding="utf-8", newline="\n")
got = []
threading.Thread(target=lambda: [got.append(json.loads(l)) for l in f], daemon=True).start()

def send(**m):
    f.write(json.dumps(m) + "\n"); f.flush()

send(t="hello", v=1, pin=pin, name="FakePhone", wdp=915, hdp=412)
time.sleep(0.3)

# マウス
send(t="mm", dx=10, dy=5)
send(t="ms", dx=0, dy=80)
send(t="tap", n=1)
send(t="btn", a="{CLICK_R}", s="down"); send(t="btn", a="{CLICK_R}", s="up")
send(t="hw", k="volup", a="down"); send(t="hw", k="volup", a="up")   # → キーボード
time.sleep(0.2)

# キーボード（既定配置: 横15キー × 縦5.7段、最上段0.7は切替ボタンと取り消し用の空き）
COLS, ROWS = 15, 5.7
def tap(cx, cy, finger=1):
    x, y = cx / COLS, cy / ROWS
    send(t="kt", a="d", id=finger, x=x, y=y); time.sleep(0.05)
    send(t="kt", a="u", id=finger, x=x, y=y); time.sleep(0.03)

tap(1, 0.7 + 3.5)              # Shift（ワンショット）
tap(1.75 + 5.5, 0.7 + 2.5)     # H → Shift+H
tap(1.75 + 7.5, 0.7 + 1.5, 2)  # I（別の指）
time.sleep(0.3)
print("received:", [m.get("t") for m in got])
s.close()
