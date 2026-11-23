package io.github.charlotte5227.phoneboard;

import android.content.Context;
import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.Rect;
import android.graphics.RectF;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.SparseArray;
import android.view.MotionEvent;
import android.view.View;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;

/**
 * 画面全体（横向き）のタッチ面。複数の指（両手の親指）に対応。
 *  - キーボード: 指ごとの位置をそのままPCへ送る（キーの判定・入力補助・表示はPC側）
 *  - マウス: PCから届いた領域（パッド/スクロール/ボタン）で操作を判定してPCへ送る
 */
public class PadView extends View {

    public interface Host {
        void send(JSONObject o);
        void sendFast(JSONObject o); // 取りこぼしても良いもの（UDP）
        void vibrate(long ms, int amp);
        void vibratePattern(long[] pattern, int amp);
        void openSetup();
    }

    static class Zone {
        String type, label, action;
        float x, y, w, h;
    }

    static class KeyBox {
        String label;
        float x, y, w, h;
    }

    /** 画面に触れている指1本分 */
    static class Ptr {
        int id, zone = -1;
        float downX, downY, lastX, lastY;
        long downT, lastT;
        boolean moved;
    }

    private final Host host;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final float dens;
    private final Paint fill = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint text = new Paint(Paint.ANTI_ALIAS_FLAG);

    // PCから受け取る設定
    private String mode = "mouse";
    private final List<Zone> mouseZones = new ArrayList<Zone>();
    private final List<Zone> gameZones = new ArrayList<Zone>();
    private final List<KeyBox> keys = new ArrayList<KeyBox>();
    private float kbCols = 15, kbRows = 5.7f;
    private boolean tapDrag = true;
    private int tapDelayMs = 220;
    private String longPress = "none";
    private int longPressMs = 550;
    private boolean momentum = true;
    private boolean miniMap = true;
    private boolean tapRight = false;
    private boolean scrollMode = false;
    private boolean connected = false;
    private String statusText = "";

    // タッチ状態
    private final SparseArray<Ptr> ptrs = new SparseArray<Ptr>();
    private int padPrimary = -1;       // カーソルを動かしている指
    private boolean twoFinger;         // パッドに2本 → スクロール
    private long twoStart;
    private boolean twoMoved;
    private boolean padIgnore;         // 2本指の後、全部離すまでパッドを無視
    private boolean second, dragging, longFired;
    private Runnable pendingClick;
    private float vx, vy;              // スクロールの慣性用 dp/ms
    private long lastMtSend;
    private int mtId = -1;
    private final DirPad dir;

    public PadView(Context c, Host host) {
        super(c);
        this.host = host;
        dens = getResources().getDisplayMetrics().density;
        text.setColor(Color.rgb(150, 150, 165));
        text.setTextAlign(Paint.Align.CENTER);
        Zone z = new Zone();
        z.type = "pad"; z.label = "パッド"; z.x = 0; z.y = 0; z.w = 1; z.h = 1;
        mouseZones.add(z);
        setKeepScreenOn(true);
        dir = new DirPad(new DirPad.Out() {
            public void send(JSONObject o) { PadView.this.host.send(o); }
            public void vibrate(long ms, int amp) { PadView.this.host.vibrate(ms, amp); }
            public void redraw() { invalidate(); }
        }, dens);
    }

    // ===== PCからの設定 =====

    public void applyCfg(JSONObject m) {
        try {
            JSONObject mo = m.optJSONObject("mouse");
            JSONObject ga = m.optJSONObject("game");
            dir.applyCfg(m.optJSONObject("dir"));
            if (ga != null) readZones(ga.optJSONArray("zones"), gameZones);
            if (mo != null) {
                readZones(mo.optJSONArray("zones"), mouseZones);
                tapDrag = mo.optBoolean("tapDrag", true);
                tapDelayMs = mo.optInt("tapDelayMs", 220);
                longPress = mo.optString("longPress", "none");
                longPressMs = mo.optInt("longPressMs", 550);
                momentum = mo.optBoolean("momentum", true);
                miniMap = mo.optBoolean("miniMap", true);
            }
            JSONObject kb = m.optJSONObject("kb");
            if (kb != null) {
                kbCols = (float) Math.max(1, kb.optDouble("cols", 15));
                kbRows = (float) Math.max(1, kb.optDouble("rows", 5.7));
                JSONArray ks = kb.optJSONArray("keys");
                keys.clear();
                if (ks != null) {
                    for (int i = 0; i < ks.length(); i++) {
                        JSONObject o = ks.getJSONObject(i);
                        KeyBox k = new KeyBox();
                        k.label = o.optString("label", "");
                        k.x = (float) o.optDouble("x", 0);
                        k.y = (float) o.optDouble("y", 0);
                        k.w = (float) o.optDouble("w", 1);
                        k.h = (float) o.optDouble("h", 1);
                        keys.add(k);
                    }
                }
            }
        } catch (Exception ignored) {
        }
        invalidate();
    }

    private static void readZones(JSONArray zs, List<Zone> out) throws Exception {
        if (zs == null) return;
        out.clear();
        for (int i = 0; i < zs.length(); i++) {
            JSONObject o = zs.getJSONObject(i);
            Zone z = new Zone();
            z.type = o.optString("type", "pad");
            z.label = o.optString("label", "");
            z.action = o.optString("action", "");
            z.x = (float) o.optDouble("x", 0);
            z.y = (float) o.optDouble("y", 0);
            z.w = (float) o.optDouble("w", 1);
            z.h = (float) o.optDouble("h", 1);
            out.add(z);
        }
    }

    /** 今の画面の領域（マウス画面 / ゲーム画面） */
    private List<Zone> zl() { return "game".equals(mode) ? gameZones : mouseZones; }

    public void setState(JSONObject m) {
        tapRight = m.optBoolean("tapRight", false);
        scrollMode = m.optBoolean("scrollMode", false);
        invalidate();
    }

    public String getMode() { return mode; }

    /** 方向入力の画面（ja / num / en） */
    public void setDirSub(String m) { dir.setSub(m); }

    public void setMode(String m) {
        if (m.equals(mode)) return;
        cancelAll();
        mode = m;
        invalidate();
    }

    public void setConnected(boolean c, String status) {
        connected = c;
        statusText = status;
        if (!c) cancelAll();
        invalidate();
    }

    @Override
    protected void onLayout(boolean changed, int l, int t, int r, int b) {
        super.onLayout(changed, l, t, r, b);
        // 画面の左右端のスワイプで「戻る」が誤動作しないようにする（Android 10以降）
        if (android.os.Build.VERSION.SDK_INT >= 29) {
            int h = b - t, w = r - l;
            int ex = (int) Math.min(h, 200 * dens);
            int edge = (int) (40 * dens);
            List<Rect> rects = new ArrayList<Rect>();
            rects.add(new Rect(0, h - ex, edge, h));
            rects.add(new Rect(w - edge, h - ex, w, h));
            setSystemGestureExclusionRects(rects);
        }
    }

    // ===== 送信ヘルパー =====

    private void send(String... kv) {
        try {
            JSONObject o = new JSONObject();
            for (int i = 0; i + 1 < kv.length; i += 2) o.put(kv[i], kv[i + 1]);
            host.send(o);
        } catch (Exception ignored) {
        }
    }

    private void sendNum(String t, double dx, double dy) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", t);
            o.put("dx", dx);
            o.put("dy", dy);
            host.sendFast(o);
        } catch (Exception ignored) {
        }
    }

    private void sendTouch(String t, String a, int id, float x, float y, int z) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", t);
            o.put("a", a);
            o.put("id", id);
            o.put("x", x / Math.max(1, getWidth()));
            o.put("y", y / Math.max(1, getHeight()));
            if (z >= -1) o.put("z", z);
            o.put("ts", SystemClock.uptimeMillis());
            // 小窓用の位置(mt)の動きはUDP、キーボードのタッチ(kt)は確実に届くTCP
            if ("mt".equals(t) && "m".equals(a)) host.sendFast(o);
            else host.send(o);
        } catch (Exception ignored) {
        }
    }

    private void sendTap(int n, String b) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", "tap");
            o.put("n", n);
            if (b != null) o.put("b", b);
            host.send(o);
        } catch (Exception ignored) {
        }
    }

    // ===== タッチ処理 =====

    private final Runnable threeHold = new Runnable() {
        public void run() {
            cancelAll();
            host.vibratePattern(new long[]{0, 60, 80, 60}, 255);
            host.openSetup();
        }
    };

    private final Runnable longPressRun = new Runnable() {
        public void run() {
            Ptr p = ptrs.get(padPrimary);
            if (p == null || p.moved || twoFinger) return;
            longFired = true;
            if ("right".equals(longPress)) {
                sendTap(1, "r");
                host.vibrate(30, 220);
            } else if ("drag".equals(longPress)) {
                send("t", "mb", "b", "l", "a", "down");
                dragging = true;
                host.vibrate(30, 220);
            }
        }
    };

    private long momLast;
    private int momFrames;

    private final Runnable momentumRun = new Runnable() {
        public void run() {
            long now = SystemClock.uptimeMillis();
            float dt = momFrames == 0 ? 16 : Math.min(40, now - momLast);
            momLast = now;
            momFrames++;
            sendNum("ms", vx * dt, vy * dt);
            vx *= 0.94f;
            vy *= 0.94f;
            if (Math.hypot(vx, vy) > 0.03 && momFrames < 200) handler.postDelayed(this, 16);
        }
    };

    private void startMomentum() {
        handler.removeCallbacks(momentumRun);
        momFrames = 0;
        handler.postDelayed(momentumRun, 16);
    }

    private void stopMomentum() {
        handler.removeCallbacks(momentumRun);
        vx = vy = 0;
    }

    private int zoneAt(float x, float y) {
        float fx = x / Math.max(1, getWidth()), fy = y / Math.max(1, getHeight());
        List<Zone> zones = zl();
        for (int i = zones.size() - 1; i >= 0; i--) {
            Zone z = zones.get(i);
            if (fx >= z.x && fx < z.x + z.w && fy >= z.y && fy < z.y + z.h) return i;
        }
        return -1;
    }

    private Zone zoneOf(Ptr p) {
        List<Zone> zones = zl();
        return p.zone >= 0 && p.zone < zones.size() ? zones.get(p.zone) : null;
    }

    /** 全ての指を離した扱いにする（モード切替・切断時） */
    private void cancelAll() {
        handler.removeCallbacks(threeHold);
        handler.removeCallbacks(longPressRun);
        dir.cancelAll();
        for (int i = 0; i < ptrs.size(); i++) {
            Ptr p = ptrs.valueAt(i);
            if ("keyboard".equals(mode)) {
                sendTouch("kt", "c", p.id, p.lastX, p.lastY, -2);
            } else if ("dir".equals(mode)) {
                // 方向入力は離した時に送るので、途中で取り消すものはない
            } else {
                Zone z = zoneOf(p);
                if (z != null && "button".equals(z.type)) send("t", "btn", "a", z.action, "s", "up");
            }
        }
        if (dragging) send("t", "mb", "b", "l", "a", "up");
        ptrs.clear();
        padPrimary = -1;
        twoFinger = false;
        padIgnore = false;
        dragging = false;
        second = false;
        longFired = false;
    }

    @Override
    public boolean onTouchEvent(MotionEvent e) {
        int act = e.getActionMasked();
        switch (act) {
            case MotionEvent.ACTION_DOWN:
            case MotionEvent.ACTION_POINTER_DOWN: {
                int i = e.getActionIndex();
                down(e.getPointerId(i), e.getX(i), e.getY(i));
                // 3本指で2秒押さえる → 接続設定画面
                if (e.getPointerCount() >= 3) handler.postDelayed(threeHold, 2000);
                return true;
            }
            case MotionEvent.ACTION_MOVE:
                for (int i = 0; i < e.getPointerCount(); i++) move(e.getPointerId(i), e.getX(i), e.getY(i));
                return true;
            case MotionEvent.ACTION_POINTER_UP:
            case MotionEvent.ACTION_UP: {
                int i = e.getActionIndex();
                handler.removeCallbacks(threeHold);
                up(e.getPointerId(i), e.getX(i), e.getY(i));
                return true;
            }
            case MotionEvent.ACTION_CANCEL:
                cancelAll();
                return true;
        }
        return true;
    }

    private void down(int id, float x, float y) {
        Ptr p = new Ptr();
        p.id = id;
        p.downX = p.lastX = x;
        p.downY = p.lastY = y;
        p.downT = p.lastT = SystemClock.uptimeMillis();
        ptrs.put(id, p);
        if (!connected) return;
        if ("keyboard".equals(mode)) {
            sendTouch("kt", "d", id, x, y, -2);
            return;
        }
        if ("dir".equals(mode)) {
            dir.down(id, x, y, getWidth());
            invalidate();
            return;
        }
        stopMomentum();
        p.zone = zoneAt(x, y);
        if (miniMap) {
            mtId = id;
            sendTouch("mt", "d", id, x, y, p.zone);
        }
        Zone z = zoneOf(p);
        if (z == null) return;
        if ("pad".equals(z.type)) {
            if (padIgnore) return;
            if (padPrimary < 0) {
                padPrimary = id;
                if (pendingClick != null) {
                    handler.removeCallbacks(pendingClick);
                    pendingClick = null;
                    second = true;
                } else {
                    second = false;
                }
                longFired = false;
                if (!"none".equals(longPress)) handler.postDelayed(longPressRun, longPressMs);
            } else if (!twoFinger && !dragging) {
                // 2本目の指 → 2本指スクロール
                twoFinger = true;
                twoStart = SystemClock.uptimeMillis();
                twoMoved = false;
                handler.removeCallbacks(longPressRun);
            }
        } else if ("scroll".equals(z.type) || "look".equals(z.type)) {
            host.vibrate(10, 160);
        } else if ("button".equals(z.type)) {
            host.vibratePattern(new long[]{0, 12, 50, 12}, 200);
            send("t", "btn", "a", z.action, "s", "down");
        }
    }

    private void move(int id, float x, float y) {
        Ptr p = ptrs.get(id);
        if (p == null || !connected) return;
        if (x == p.lastX && y == p.lastY) return;
        long now = SystemClock.uptimeMillis();
        if ("keyboard".equals(mode)) {
            p.lastX = x;
            p.lastY = y;
            sendTouch("kt", "m", id, x, y, -2);
            return;
        }
        if ("dir".equals(mode)) {
            p.lastX = x;
            p.lastY = y;
            dir.move(id, x, y);
            invalidate();
            return;
        }
        if (miniMap && id == mtId && now - lastMtSend > 30) {
            lastMtSend = now;
            sendTouch("mt", "m", id, x, y, p.zone);
        }
        float dx = (x - p.lastX) / dens, dy = (y - p.lastY) / dens;
        boolean wasMoved = p.moved;
        if (!p.moved && Math.hypot(x - p.downX, y - p.downY) / dens >= 8) p.moved = true;
        Zone z = zoneOf(p);
        if (z != null && "pad".equals(z.type) && !padIgnore) {
            if (twoFinger) {
                // 2本指: 各指の動きの半分ずつ → 平均の動きでスクロール
                if (p.moved) twoMoved = true;
                if (twoMoved) sendNum("ms", dx / 2, dy / 2);
            } else if (id == padPrimary) {
                if (!p.moved) return; // 指のぶれは無視（タップ判定のため）。lastX は更新しない
                if (!wasMoved) {
                    // 動き始め: 触れた位置からの移動分をまとめて送る
                    dx = (x - p.downX) / dens;
                    dy = (y - p.downY) / dens;
                    handler.removeCallbacks(longPressRun);
                    if (second && tapDrag && !dragging) {
                        send("t", "mb", "b", "l", "a", "down");
                        dragging = true;
                    }
                }
                if (scrollMode && !dragging) sendNum("ms", dx, dy);
                else sendNum("mm", dx, dy);
            }
        } else if (z != null && "look".equals(z.type)) {
            // ゲームの視点: 指のぶれも含めてそのまま送る（素早いタップは左クリック）
            sendNum("lm", dx, dy);
        } else if (z != null && "scroll".equals(z.type)) {
            sendNum("ms", dx, dy);
            float dt = Math.max(1, now - p.lastT);
            vx = vx * 0.5f + (dx / dt) * 0.5f;
            vy = vy * 0.5f + (dy / dt) * 0.5f;
        }
        p.lastX = x;
        p.lastY = y;
        p.lastT = now;
    }

    private void up(int id, float x, float y) {
        Ptr p = ptrs.get(id);
        if (p == null) return;
        if (connected) move(id, x, y);
        ptrs.remove(id);
        if (!connected) return;
        long now = SystemClock.uptimeMillis();
        if ("keyboard".equals(mode)) {
            sendTouch("kt", "u", id, x, y, -2);
            return;
        }
        if ("dir".equals(mode)) {
            dir.up(id, x, y);
            invalidate();
            return;
        }
        if (miniMap && id == mtId) sendTouch("mt", "u", id, x, y, p.zone);
        Zone z = zoneOf(p);
        if (z == null) return;
        if ("pad".equals(z.type)) {
            padUp(p, now);
        } else if ("look".equals(z.type)) {
            // 動かさずに素早く離した → 左クリック（「タップ=右」の切替は無視して常に左）
            if (!p.moved && now - p.downT < 300) sendTap(1, "l");
        } else if ("scroll".equals(z.type)) {
            if (momentum && now - p.lastT < 60 && Math.hypot(vx, vy) > 0.25) startMomentum();
        } else if ("button".equals(z.type)) {
            send("t", "btn", "a", z.action, "s", "up");
        }
    }

    private int padFingers() {
        int n = 0;
        for (int i = 0; i < ptrs.size(); i++) {
            Zone z = zoneOf(ptrs.valueAt(i));
            if (z != null && "pad".equals(z.type)) n++;
        }
        return n;
    }

    private void padUp(Ptr p, long now) {
        if (padIgnore) {
            if (padFingers() == 0) padIgnore = false;
            return;
        }
        if (twoFinger) {
            // 2本指で軽くトン → 右クリック
            if (!twoMoved && now - twoStart < 300) sendTap(1, "r");
            twoFinger = false;
            padPrimary = -1;
            second = false;
            padIgnore = padFingers() > 0; // 残った指は離すまで無視（カーソルが飛ばないように）
            return;
        }
        if (p.id != padPrimary) return;
        padPrimary = -1;
        handler.removeCallbacks(longPressRun);
        boolean tap = !p.moved && now - p.downT < 350;
        if (dragging) {
            send("t", "mb", "b", "l", "a", "up");
        } else if (longFired) {
            // 長押しで処理済み
        } else if (tap && second) {
            sendTap(2, null); // ダブルクリック
        } else if (tap && tapDrag) {
            // ダブルタップドラッグの判定のため少し待ってからクリック
            pendingClick = new Runnable() {
                public void run() {
                    pendingClick = null;
                    sendTap(1, null);
                }
            };
            handler.postDelayed(pendingClick, tapDelayMs);
        } else if (tap) {
            sendTap(1, null);
        } else if (second && !p.moved) {
            sendTap(1, null); // 1回目のタップ分
        }
        dragging = false;
        second = false;
        longFired = false;
    }

    // ===== 描画（スマホは見ない前提なので、設定確認用の簡素な表示） =====

    @Override
    protected void onDraw(Canvas c) {
        int w = getWidth(), h = getHeight();
        c.drawColor(Color.BLACK);
        float r = 8 * dens;
        if ("dir".equals(mode)) {
            dir.draw(c, w, h);
        } else if ("keyboard".equals(mode)) {
            float sx = w / kbCols, sy = h / kbRows;
            text.setTextSize(Math.min(sx, sy) * 0.32f);
            for (KeyBox k : keys) {
                RectF rf = new RectF(k.x * sx + 2 * dens, k.y * sy + 2 * dens, (k.x + k.w) * sx - 2 * dens, (k.y + k.h) * sy - 2 * dens);
                fill.setColor(Color.rgb(30, 31, 38));
                c.drawRoundRect(rf, r, r, fill);
                c.drawText(k.label, rf.centerX(), rf.centerY() + text.getTextSize() * 0.35f, text);
            }
        } else {
            text.setTextSize(15 * dens);
            for (Zone z : zl()) {
                RectF rf = new RectF(z.x * w + 3 * dens, z.y * h + 3 * dens, (z.x + z.w) * w - 3 * dens, (z.y + z.h) * h - 3 * dens);
                int col = Color.rgb(24, 24, 30);
                if ("scroll".equals(z.type)) col = Color.rgb(18, 30, 42);
                if ("look".equals(z.type)) col = Color.rgb(16, 34, 24);
                if ("button".equals(z.type)) col = Color.rgb(34, 26, 44);
                fill.setColor(col);
                c.drawRoundRect(rf, r, r, fill);
                String label = z.label;
                if ("pad".equals(z.type)) {
                    if (tapRight) label += " (タップ=右)";
                    if (scrollMode) label += " (スクロール)";
                }
                c.drawText(label, rf.centerX(), rf.centerY(), text);
            }
        }
        text.setTextSize(12 * dens);
        text.setTextAlign(Paint.Align.RIGHT);
        c.drawText((connected ? "● " : "○ ") + statusText, w - 10 * dens, h - 8 * dens, text);
        text.setTextAlign(Paint.Align.CENTER);
    }
}
