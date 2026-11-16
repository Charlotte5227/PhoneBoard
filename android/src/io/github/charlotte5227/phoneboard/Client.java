package io.github.charlotte5227.phoneboard;

import android.content.Context;
import android.net.DhcpInfo;
import android.net.wifi.WifiManager;
import android.os.Handler;
import android.os.Looper;

import org.json.JSONObject;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.io.OutputStreamWriter;
import java.io.Writer;
import java.net.DatagramPacket;
import java.net.DatagramSocket;
import java.net.InetAddress;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.SocketTimeoutException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;

/** PCとの通信（TCP: 改行区切りJSON / UDP: 自動検出）。自動再接続する。 */
public class Client {
    public static final int DEFAULT_PORT = 39500;

    public interface Listener {
        void onStatus(String text, boolean connected);
        void onMessage(JSONObject m);
        void onDenied();
    }

    public static class Found {
        public String host;
        public int port;
        public String name;
    }

    private final Context ctx;
    private final Listener listener;
    private final Handler main = new Handler(Looper.getMainLooper());
    private final LinkedBlockingQueue<String> outq = new LinkedBlockingQueue<String>();

    private volatile String host;
    private volatile int port = DEFAULT_PORT;
    private volatile String pin = "";
    private volatile String pcName = "";
    private volatile String deviceName = "Android";
    private volatile float wdp = 400, hdp = 860;
    private volatile boolean running;
    private volatile boolean connected;
    private volatile Socket sock;
    private Thread thread;

    public Client(Context ctx, Listener l) {
        this.ctx = ctx.getApplicationContext();
        this.listener = l;
    }

    public boolean isConnected() { return connected; }
    public String getPcName() { return pcName; }

    public void setDevice(String name, float wdp, float hdp) {
        this.deviceName = name;
        this.wdp = wdp;
        this.hdp = hdp;
        if (connected) {
            try {
                JSONObject o = new JSONObject();
                o.put("t", "size");
                o.put("wdp", wdp);
                o.put("hdp", hdp);
                send(o);
            } catch (Exception ignored) {
            }
        }
    }

    public void setTarget(String host, int port, String pin, String pcName) {
        this.host = host;
        this.port = port > 0 ? port : DEFAULT_PORT;
        this.pin = pin == null ? "" : pin;
        if (pcName != null) this.pcName = pcName;
        closeSock();
        if (thread != null) thread.interrupt();
    }

    public synchronized void start() {
        if (running) return;
        running = true;
        thread = new Thread(new Runnable() {
            public void run() { loop(); }
        }, "pb-net");
        thread.start();
    }

    public synchronized void stop() {
        running = false;
        closeSock();
        if (thread != null) thread.interrupt();
    }

    public void send(JSONObject o) {
        if (connected) outq.offer(o.toString());
    }

    private void closeSock() {
        Socket s = sock;
        sock = null;
        if (s != null) {
            try { s.close(); } catch (Exception ignored) { }
        }
    }

    private void status(final String text, final boolean ok) {
        main.post(new Runnable() {
            public void run() { listener.onStatus(text, ok); }
        });
    }

    private static void sleepMs(long ms) {
        try { Thread.sleep(ms); } catch (InterruptedException ignored) { }
    }

    private void loop() {
        int fails = 0;
        while (running) {
            final String h = host;
            if (h == null || h.length() == 0) {
                status("PCが未設定です", false);
                sleepMs(1000);
                continue;
            }
            boolean denied = false;
            final Socket s = new Socket();
            try {
                status("接続中… " + h, false);
                s.setTcpNoDelay(true);
                s.connect(new InetSocketAddress(h, port), 3000);
                s.setSoTimeout(15000);
                sock = s;
                outq.clear();
                final Writer out = new OutputStreamWriter(s.getOutputStream(), "UTF-8");
                BufferedReader in = new BufferedReader(new InputStreamReader(s.getInputStream(), "UTF-8"));
                JSONObject hello = new JSONObject();
                hello.put("t", "hello");
                hello.put("v", 1);
                hello.put("pin", pin);
                hello.put("name", deviceName);
                hello.put("wdp", wdp);
                hello.put("hdp", hdp);
                out.write(hello.toString());
                out.write('\n');
                out.flush();

                Thread writer = new Thread(new Runnable() {
                    public void run() {
                        long lastSend = System.currentTimeMillis();
                        try {
                            while (sock == s) {
                                String line = outq.poll(500, TimeUnit.MILLISECONDS);
                                long now = System.currentTimeMillis();
                                if (line == null && now - lastSend > 3000) {
                                    line = "{\"t\":\"ping\"}";
                                }
                                if (line != null) {
                                    StringBuilder sb = new StringBuilder(line).append('\n');
                                    // 溜まっている分はまとめて送る
                                    String more;
                                    while ((more = outq.poll()) != null) sb.append(more).append('\n');
                                    out.write(sb.toString());
                                    out.flush();
                                    lastSend = now;
                                }
                            }
                        } catch (Exception e) {
                            closeSock();
                        }
                    }
                }, "pb-writer");
                writer.start();

                String line;
                while (running && sock == s && (line = in.readLine()) != null) {
                    final JSONObject m;
                    try { m = new JSONObject(line); } catch (Exception e) { continue; }
                    String t = m.optString("t");
                    if ("welcome".equals(t)) {
                        connected = true;
                        fails = 0;
                        pcName = m.optString("host", pcName);
                        status("接続済み: " + pcName, true);
                    } else if ("denied".equals(t)) {
                        denied = true;
                        main.post(new Runnable() {
                            public void run() { listener.onDenied(); }
                        });
                        break;
                    }
                    main.post(new Runnable() {
                        public void run() { listener.onMessage(m); }
                    });
                }
            } catch (SocketTimeoutException e) {
                // 応答なし
            } catch (Exception e) {
                // 接続失敗
            }
            if (sock == s) closeSock();
            try { s.close(); } catch (Exception ignored) { }
            boolean was = connected;
            connected = false;
            if (was) status("切断されました。再接続中…", false);
            if (denied) {
                status("PINが違います。PC画面のPINを入力してください", false);
                // PINを直すまで待つ
                String p = pin;
                while (running && p.equals(pin) && h.equals(host)) sleepMs(500);
                continue;
            }
            fails++;
            if (!running) break;
            // IPが変わった可能性 → 同じ名前のPCを探す
            if (fails >= 2 && pcName != null && pcName.length() > 0) {
                List<Found> list = discover(ctx, 1200);
                for (Found f : list) {
                    if (pcName.equals(f.name) && !f.host.equals(host)) {
                        host = f.host;
                        port = f.port;
                        main.post(new Runnable() {
                            public void run() { listener.onStatus("PCのアドレスが変わったため更新しました", false); }
                        });
                    }
                }
            }
            sleepMs(Math.min(1000 + fails * 300, 4000));
        }
        connected = false;
    }

    /** 同じLAN内のPCを探す（数秒ブロックするので別スレッドで呼ぶ）。 */
    public static List<Found> discover(Context ctx, int timeoutMs) {
        List<Found> out = new ArrayList<Found>();
        DatagramSocket ds = null;
        try {
            ds = new DatagramSocket();
            ds.setBroadcast(true);
            ds.setSoTimeout(250);
            byte[] q = "PHONEBOARD?".getBytes("UTF-8");
            List<InetAddress> targets = new ArrayList<InetAddress>();
            targets.add(InetAddress.getByName("255.255.255.255"));
            try {
                WifiManager wm = (WifiManager) ctx.getApplicationContext().getSystemService(Context.WIFI_SERVICE);
                DhcpInfo d = wm != null ? wm.getDhcpInfo() : null;
                if (d != null && d.ipAddress != 0) {
                    int mask = d.netmask != 0 ? d.netmask : 0x00ffffff;
                    int bc = (d.ipAddress & mask) | ~mask;
                    byte[] b = new byte[]{(byte) bc, (byte) (bc >> 8), (byte) (bc >> 16), (byte) (bc >> 24)};
                    targets.add(InetAddress.getByAddress(b));
                }
            } catch (Exception ignored) {
            }
            long end = System.currentTimeMillis() + timeoutMs;
            long nextSend = 0;
            byte[] buf = new byte[512];
            while (System.currentTimeMillis() < end) {
                if (System.currentTimeMillis() >= nextSend) {
                    for (InetAddress a : targets) {
                        try { ds.send(new DatagramPacket(q, q.length, a, DEFAULT_PORT)); } catch (Exception ignored) { }
                    }
                    nextSend = System.currentTimeMillis() + 400;
                }
                DatagramPacket p = new DatagramPacket(buf, buf.length);
                try {
                    ds.receive(p);
                } catch (SocketTimeoutException e) {
                    continue;
                }
                String r = new String(p.getData(), 0, p.getLength(), "UTF-8");
                if (!r.startsWith("PHONEBOARD!")) continue;
                String[] parts = r.split("\\|");
                Found f = new Found();
                f.host = p.getAddress().getHostAddress();
                f.port = parts.length > 1 ? Integer.parseInt(parts[1].trim()) : DEFAULT_PORT;
                f.name = parts.length > 2 ? parts[2] : f.host;
                boolean dup = false;
                for (Found g : out) if (g.host.equals(f.host)) dup = true;
                if (!dup) out.add(f);
            }
        } catch (Exception ignored) {
        } finally {
            if (ds != null) ds.close();
        }
        return out;
    }
}
