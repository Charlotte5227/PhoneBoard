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
import android.view.MotionEvent;
import android.view.View;

import org.json.JSONArray;
import org.json.JSONObject;

import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.List;

/**
 * 画面全体のタッチ面。指1本（最初に触れた指）だけを使う。
 *  - マウスモード: PCから届いた領域（パッド/スクロール/ボタン）で操作を判定してPCへ送る
 *  - キーボードモード: 触れている位置をそのままPCへ送る（キー判定・表示はPC側）
 */
public class PadView extends View {

    public interface Host {
        void send(JSONObject o);
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

    private final Host host;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final float dens;
    private final Paint fill = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint stroke = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint text = new Paint(Paint.ANTI_ALIAS_FLAG);

    // PCから受け取る設定
    private String mode = "mouse";
    private final List<Zone> zones = new ArrayList<Zone>();
    private final List<KeyBox> keys = new ArrayList<KeyBox>();
    private int kbCols = 5, kbRows = 4;
    private float rx = 0, ry = 0.42f, rw = 1, rh = 0.58f;
    private String kbTitle = "";
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
    private int activeId = -1;
    private int zoneIdx = -1;
    private float downX, downY, lastX, lastY;
    private long downT, lastMoveT;
    private boolean moved, second, dragging, longFired;
    private float vx, vy; // スクロール速度 dp/ms
    private long lastMtSend;
    private long evTime;
    private Runnable pendingClick;

    public PadView(Context c, Host host) {
        super(c);
        this.host = host;
        dens = getResources().getDisplayMetrics().density;
        text.setColor(Color.rgb(150, 150, 165));
        text.setTextAlign(Paint.Align.CENTER);
        stroke.setStyle(Paint.Style.STROKE);
        stroke.setStrokeWidth(dens);
        // 既定（PC接続前）
        Zone z = new Zone();
        z.type = "pad"; z.label = "パッド"; z.x = 0; z.y = 0; z.w = 1; z.h = 1;
        zones.add(z);
        setKeepScreenOn(true);
    }

    // ===== PCからの設定 =====

    public void applyCfg(JSONObject m) {
        try {
            JSONObject mo = m.optJSONObject("mouse");
            if (mo != null) {
                JSONArray zs = mo.optJSONArray("zones");
                if (zs != null) {
                    zones.clear();
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
                        zones.add(z);
                    }
                }
                tapDrag = mo.optBoolean("tapDrag", true);
                tapDelayMs = mo.optInt("tapDelayMs", 220);
                longPress = mo.optString("longPress", "none");
                longPressMs = mo.optInt("longPressMs", 550);
                momentum = mo.optBoolean("momentum", true);
                miniMap = mo.optBoolean("miniMap", true);
            }
            JSONObject kb = m.optJSONObject("kb");
            if (kb != null) {
                kbTitle = kb.optString("title", "");
                kbCols = Math.max(1, kb.optInt("cols", 5));
                kbRows = Math.max(1, kb.optInt("rows", 4));
                JSONObject r = kb.optJSONObject("region");
                if (r != null) {
                    rx = (float) r.optDouble("x", 0);
                    ry = (float) r.optDouble("y", 0.42);
                    rw = (float) r.optDouble("w", 1);
                    rh = (float) r.optDouble("h", 0.58);
                }
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

    public void setState(JSONObject m) {
        tapRight = m.optBoolean("tapRight", false);
        scrollMode = m.optBoolean("scrollMode", false);
        invalidate();
    }

    public void setMode(String m) {
        if (m.equals(mode)) return;
        cancelTouch();
        mode = m;
        invalidate();
    }

    public void setConnected(boolean c, String status) {
        connected = c;
        statusText = status;
        if (!c) cancelTouch();
        invalidate();
    }

    @Override
    protected void onLayout(boolean changed, int l, int t, int r, int b) {
        super.onLayout(changed, l, t, r, b);
        // Android 10以降: 画面端のスワイプで「戻る」が誤動作しないようにする（下側の左右端）
        try {
            int h = b - t, w = r - l;
            int ex = (int) (200 * dens);
            int edge = (int) (40 * dens);
            List<Rect> rects = new ArrayList<Rect>();
            rects.add(new Rect(0, Math.max(0, h - ex), edge, h));
            rects.add(new Rect(w - edge, Math.max(0, h - ex), w, h));
            Method mth = View.class.getMethod("setSystemGestureExclusionRects", List.class);
            mth.invoke(this, rects);
        } catch (Throwable ignored) {
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

    private void sendNum(String t, String k1, double v1, String k2, double v2) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", t);
            o.put(k1, v1);
            o.put(k2, v2);
            host.send(o);
        } catch (Exception ignored) {
        }
    }

    private void sendTouch(String t, String a, float x, float y, int z) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", t);
            o.put("a", a);
            o.put("x", x / Math.max(1, getWidth()));
            o.put("y", y / Math.max(1, getHeight()));
            if (z >= -1) o.put("z", z);
            if ("kt".equals(t)) o.put("ts", evTime);
            host.send(o);
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

    private final Runnable cornerHold = new Runnable() {
        public void run() {
            cancelTouch();
            host.vibratePattern(new long[]{0, 60, 80, 60}, 255);
            host.openSetup();
        }
    };

    private final Runnable longPressRun = new Runnable() {
        public void run() {
            if (moved || activeId < 0) return;
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
            sendNum("ms", "dx", vx * dt, "dy", vy * dt);
            vx *= 0.94f;
            vy *= 0.94f;
            if (Math.hypot(vx, vy) > 0.03 && momFrames < 200) {
                handler.postDelayed(this, 16);
            }
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
        for (int i = zones.size() - 1; i >= 0; i--) {
            Zone z = zones.get(i);
            if (fx >= z.x && fx < z.x + z.w && fy >= z.y && fy < z.y + z.h) return i;
        }
        return -1;
    }

    private void cancelTouch() {
        handler.removeCallbacks(cornerHold);
        handler.removeCallbacks(longPressRun);
        if (activeId >= 0) {
            if ("keyboard".equals(mode)) {
                send("t", "kt", "a", "c");
            } else {
                if (dragging) send("t", "mb", "b", "l", "a", "up");
                if (zoneIdx >= 0 && zoneIdx < zones.size() && "button".equals(zones.get(zoneIdx).type)) {
                    send("t", "btn", "a", zones.get(zoneIdx).action, "s", "up");
                }
            }
        }
        activeId = -1;
        dragging = false;
        second = false;
        longFired = false;
    }

    @Override
    public boolean onTouchEvent(MotionEvent e) {
        int act = e.getActionMasked();
        evTime = e.getEventTime();
        switch (act) {
            case MotionEvent.ACTION_DOWN: {
                activeId = e.getPointerId(0);
                float x = e.getX(0), y = e.getY(0);
                if (x < getWidth() * 0.14f && y < getHeight() * 0.08f) {
                    handler.postDelayed(cornerHold, 2000);
                }
                down(x, y);
                return true;
            }
            case MotionEvent.ACTION_MOVE: {
                if (activeId < 0) return true;
                int i = e.findPointerIndex(activeId);
                if (i < 0) return true;
                float x = e.getX(i), y = e.getY(i);
                if (Math.hypot(x - downX, y - downY) > 24 * dens) handler.removeCallbacks(cornerHold);
                move(x, y);
                return true;
            }
            case MotionEvent.ACTION_POINTER_UP: {
                int i = e.getActionIndex();
                if (e.getPointerId(i) == activeId) {
                    handler.removeCallbacks(cornerHold);
                    up(e.getX(i), e.getY(i));
                    activeId = -1;
                }
                return true;
            }
            case MotionEvent.ACTION_UP: {
                handler.removeCallbacks(cornerHold);
                if (activeId >= 0) {
                    int i = e.findPointerIndex(activeId);
                    if (i < 0) i = 0;
                    up(e.getX(i), e.getY(i));
                }
                activeId = -1;
                return true;
            }
            case MotionEvent.ACTION_CANCEL:
                cancelTouch();
                return true;
        }
        return true;
    }

    private void down(float x, float y) {
        downX = lastX = x;
        downY = lastY = y;
        downT = lastMoveT = SystemClock.uptimeMillis();
        moved = false;
        longFired = false;
        dragging = false;
        if (!connected) return;
        if ("keyboard".equals(mode)) {
            sendTouch("kt", "d", x, y, -2);
            return;
        }
        stopMomentum();
        zoneIdx = zoneAt(x, y);
        if (miniMap) sendTouch("mt", "d", x, y, zoneIdx);
        if (zoneIdx < 0) return;
        Zone z = zones.get(zoneIdx);
        if ("pad".equals(z.type)) {
            if (pendingClick != null) {
                handler.removeCallbacks(pendingClick);
                pendingClick = null;
                second = true;
            } else {
                second = false;
            }
            if (!"none".equals(longPress)) handler.postDelayed(longPressRun, longPressMs);
        } else if ("scroll".equals(z.type)) {
            host.vibrate(10, 160);
        } else if ("button".equals(z.type)) {
            host.vibratePattern(new long[]{0, 12, 50, 12}, 200);
            send("t", "btn", "a", z.action, "s", "down");
        }
    }

    private void move(float x, float y) {
        if (!connected) return;
        long now = SystemClock.uptimeMillis();
        if ("keyboard".equals(mode)) {
            sendTouch("kt", "m", x, y, -2);
            return;
        }
        if (miniMap && now - lastMtSend > 30) {
            lastMtSend = now;
            sendTouch("mt", "m", x, y, zoneIdx);
        }
        if (zoneIdx < 0) return;
        Zone z = zones.get(zoneIdx);
        float dx = (x - lastX) / dens, dy = (y - lastY) / dens;
        if ("pad".equals(z.type)) {
            if (!moved) {
                if (Math.hypot(x - downX, y - downY) / dens < 8) return;
                moved = true;
                handler.removeCallbacks(longPressRun);
                dx = (x - downX) / dens;
                dy = (y - downY) / dens;
                if (second && tapDrag && !dragging) {
                    send("t", "mb", "b", "l", "a", "down");
                    dragging = true;
                }
            }
            if (scrollMode && !dragging) {
                sendNum("ms", "dx", dx, "dy", dy);
            } else {
                sendNum("mm", "dx", dx, "dy", dy);
            }
        } else if ("scroll".equals(z.type)) {
            sendNum("ms", "dx", dx, "dy", dy);
            float dt = Math.max(1, now - lastMoveT);
            vx = vx * 0.5f + (dx / dt) * 0.5f;
            vy = vy * 0.5f + (dy / dt) * 0.5f;
        }
        lastX = x;
        lastY = y;
        lastMoveT = now;
    }

    private void up(float x, float y) {
        handler.removeCallbacks(longPressRun);
        if (!connected) return;
        long now = SystemClock.uptimeMillis();
        if ("keyboard".equals(mode)) {
            sendTouch("kt", "u", x, y, -2);
            return;
        }
        if (miniMap) sendTouch("mt", "u", x, y, zoneIdx);
        if (zoneIdx < 0) return;
        Zone z = zones.get(zoneIdx);
        if ("pad".equals(z.type)) {
            boolean tap = !moved && now - downT < 350;
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
            } else if (second && !moved) {
                sendTap(1, null); // 1回目のタップ分
            }
            dragging = false;
            second = false;
            longFired = false;
        } else if ("scroll".equals(z.type)) {
            if (momentum && now - lastMoveT < 60 && Math.hypot(vx, vy) > 0.25) {
                startMomentum();
            }
        } else if ("button".equals(z.type)) {
            send("t", "btn", "a", z.action, "s", "up");
        }
    }

    // ===== 描画（スマホは見ない前提なので、設定確認用の簡素な表示） =====

    @Override
    protected void onDraw(Canvas c) {
        int w = getWidth(), h = getHeight();
        c.drawColor(Color.BLACK);
        float r = 10 * dens;
        if ("keyboard".equals(mode)) {
            float ox = rx * w, oy = ry * h, ow = rw * w, oh = rh * h;
            float cw = ow / kbCols, ch = oh / kbRows;
            text.setTextSize(Math.min(cw, ch) * 0.3f);
            for (KeyBox k : keys) {
                RectF rf = new RectF(ox + k.x * cw + 3 * dens, oy + k.y * ch + 3 * dens,
                        ox + (k.x + k.w) * cw - 3 * dens, oy + (k.y + k.h) * ch - 3 * dens);
                fill.setColor(Color.rgb(32, 33, 40));
                c.drawRoundRect(rf, r, r, fill);
                String[] lines = k.label.split("\n");
                float lh = text.getTextSize() * 1.15f;
                float y0 = rf.centerY() - (lines.length - 1) * lh / 2 + text.getTextSize() * 0.35f;
                for (int i = 0; i < lines.length; i++) c.drawText(lines[i], rf.centerX(), y0 + i * lh, text);
            }
            text.setTextSize(14 * dens);
            c.drawText("キーボード: " + kbTitle + "（PC画面を見て操作）", w / 2f, oy - 12 * dens, text);
        } else {
            text.setTextSize(15 * dens);
            for (Zone z : zones) {
                RectF rf = new RectF(z.x * w + 3 * dens, z.y * h + 3 * dens, (z.x + z.w) * w - 3 * dens, (z.y + z.h) * h - 3 * dens);
                int col = Color.rgb(24, 24, 30);
                if ("scroll".equals(z.type)) col = Color.rgb(18, 30, 42);
                if ("button".equals(z.type)) col = Color.rgb(34, 26, 44);
                fill.setColor(col);
                c.drawRoundRect(rf, r, r, fill);
                String label = z.label;
                if ("pad".equals(z.type)) {
                    if (tapRight) label += "\n(タップ=右)";
                    if (scrollMode) label += "\n(スクロール)";
                }
                String[] lines = label.split("\n");
                float lh = text.getTextSize() * 1.2f;
                for (int i = 0; i < lines.length; i++) {
                    c.drawText(lines[i], rf.centerX(), rf.centerY() + (i - (lines.length - 1) / 2f) * lh, text);
                }
            }
        }
        text.setTextSize(12 * dens);
        text.setTextAlign(Paint.Align.LEFT);
        c.drawText((connected ? "● " : "○ ") + statusText, 10 * dens, h - 8 * dens, text);
        text.setTextAlign(Paint.Align.CENTER);
        stroke.setColor(Color.rgb(50, 50, 60));
        c.drawRect(1, 1, w * 0.14f, h * 0.08f, stroke);
    }
}
