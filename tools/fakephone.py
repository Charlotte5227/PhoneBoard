#!/usr/bin/env python3
"""PCソフトの動作確認用の模擬スマホ。
使い方: python3 fakephone.py <PCのIP> <PIN>
"""
import json, socket, sys, time, threading

host = sys.argv[1] if len(sys.argv) > 1 else "127.0.0.1"
pin = sys.argv[2] if len(sys.argv) > 2 else ""

# 自動検出
u = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
u.settimeout(1)
u.sendto(b"PHONEBOARD?", (host, 39500))
try:
    print("discovery:", u.recvfrom(256)[0].decode())
except socket.timeout:
    print("discovery: no reply")

s = socket.create_connection((host, 39500))
f = s.makefile("rw", encoding="utf-8", newline="\n")
recv = []

def reader():
    for line in f:
        recv.append(json.loads(line))

def send(**m):
    f.write(json.dumps(m) + "\n"); f.flush()

send(t="hello", v=1, pin=pin, name="FakePhone", wdp=400, hdp=860)
threading.Thread(target=reader, daemon=True).start()
time.sleep(0.3)

# マウス
send(t="mm", dx=10, dy=5)
send(t="ms", dx=0, dy=80)
send(t="tap", n=1)
send(t="btn", a="{CLICK_R}", s="down"); send(t="btn", a="{CLICK_R}", s="up")
send(t="act", a="{TAPMODE}")
send(t="tap", n=1)          # 右クリック（ワンショットで左に戻る）
send(t="hw", k="volup", a="down"); send(t="hw", k="volup", a="up")   # → キーボード
time.sleep(0.2)

# キーボード: 「か」キーを素早くタップ
R = dict(x=0, y=0.42, w=1, h=0.58)
def key(col, row, cols=5, rows=4):
    return R["x"] + (col + .5) / cols * R["w"], R["y"] + (row + .5) / rows * R["h"]
x, y = key(2, 0)
send(t="kt", a="d", x=x, y=y); time.sleep(0.05); send(t="kt", a="u", x=x, y=y)
# 「は」で止めて → 右にはじく（へ）
x, y = key(3, 1)
send(t="kt", a="d", x=x, y=y); time.sleep(0.45)
send(t="kt", a="m", x=x + 0.05, y=y); time.sleep(0.05); send(t="kt", a="u", x=x + 0.1, y=y)
time.sleep(0.3)
for m in recv:
    print("<-", m if m.get("t") != "cfg" else {"t": "cfg", "zones": len(m["mouse"]["zones"])})
s.close()
