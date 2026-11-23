package io.github.charlotte5227.phoneboard;

import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.Path;
import android.os.Handler;
import android.os.Looper;
import android.os.SystemClock;
import android.util.SparseArray;

import org.json.JSONArray;
import org.json.JSONObject;

import java.util.ArrayList;
import java.util.List;

/**
 * 方向入力（画面を見ずに、はじく方向で打つ）のタッチ判定。
 *  - 日本語・数字: 画面の左半分/右半分のどこに触れてもよく、触れた点からはじいた方向（8方向＋トン）をPCへ送る
 *  - 英字: 画面全体に一筆書き。軌跡をPCへ送る（判定はPC）
 * 文字の決定・音・表示はPC側（dir.go）。
 */
class DirPad {

    interface Out {
        void send(JSONObject o);
        void vibrate(long ms, int amp);
        void redraw();
    }

    static class F {
        int id;
        boolean left;
        float x0, y0, x, y;          // px
        float maxD;                  // 触れた点からの一番遠い距離（dp）
        float farX, farY;            // その点（px）
        long t0;
        boolean seIn, bend;          // ↘ に入った / そこから左へ曲げた
        float seX;                   // ↘ に入ってからの一番右（px）
        float len;                   // なぞった長さ（dp）
        boolean others, holder;      // 英字: 他の指が触れていた（=大文字）/ 大文字用に押さえている指
        int corner;                  // 曲げの起点にした方向（8=↖ 6=↙、0=なし）
        boolean ulFrozen;            // 曲げた（基準点を固定）
        float ax, ay;                // 角の行き着いた点（曲げの基準）
        float px, py;                // 直前の位置
        String ulKind;               // 曲げた操作: bs / enter / tab / mshift / mwin / mctrl
        boolean repeated;            // 削除を押さえたまま連続で送った
        Runnable rep;                // 連続削除のタイマー
        final List<float[]> pts = new ArrayList<float[]>();
    }

    private final Out out;
    private final float dens;
    String sub = "ja";
    String inKind = "flick"; // flick=はじき / stroke=一筆書き（英字・修飾キーの後の1文字）
    private Runnable pendingTap;

    boolean stroke() { return "stroke".equals(inKind); }
    float flickDp = 16, bendDp = 30;
    int repeatDelayMs = 450, repeatIntervalMs = 60;
    private final Handler handler = new Handler(Looper.getMainLooper());
    private final SparseArray<F> fs = new SparseArray<F>();
    private long lastTapT0 = -1, lastTapT1 = -1;
    private final Paint paint = new Paint(Paint.ANTI_ALIAS_FLAG);
    private final Paint text = new Paint(Paint.ANTI_ALIAS_FLAG);

    DirPad(Out out, float dens) {
        this.out = out;
        this.dens = dens;
        text.setTextAlign(Paint.Align.CENTER);
    }

    void applyCfg(JSONObject d) {
        if (d == null) return;
        flickDp = (float) d.optDouble("flickDp", flickDp);
        bendDp = (float) d.optDouble("bendDp", bendDp);
        repeatDelayMs = d.optInt("repeatDelayMs", repeatDelayMs);
        repeatIntervalMs = Math.max(20, d.optInt("repeatIntervalMs", repeatIntervalMs));
    }

    void setSub(String m, String in) {
        sub = m;
        inKind = in != null && in.length() > 0 ? in : ("en".equals(m) ? "stroke" : "flick");
        cancelAll();
        out.redraw();
    }

    void cancelAll() {
        for (int i = 0; i < fs.size(); i++) stopRepeat(fs.valueAt(i));
        fs.clear();
    }

    private void stopRepeat(F f) {
        if (f.rep != null) {
            handler.removeCallbacks(f.rep);
            f.rep = null;
        }
    }

    /** 削除の曲げのまま押さえ続けたら、連続で削除を送る */
    private void startRepeat(final F f) {
        stopRepeat(f);
        f.rep = new Runnable() {
            public void run() {
                if (fs.get(f.id) != f || !"bs".equals(f.ulKind)) return;
                f.repeated = true;
                sendFlick("R", 8, "bs");
                handler.postDelayed(this, repeatIntervalMs);
            }
        };
        handler.postDelayed(f.rep, repeatDelayMs);
    }

    /** 方向の番号: 0=トン 1=↑ 2=↗ 3=→ 4=↘ 5=↓ 6=↙ 7=← 8=↖ */
    static int dirOf(float dx, float dy) {
        double deg = Math.toDegrees(Math.atan2(-dy, dx)); // 上が正
        int s = (int) Math.round(deg / 45.0);
        s = ((s % 8) + 8) % 8; // 0=→ 1=↗ 2=↑ 3=↖ 4=← 5=↙ 6=↓ 7=↘
        int[] map = {3, 2, 1, 8, 7, 6, 5, 4};
        return map[s];
    }

    void down(int id, float x, float y, int width) {
        F f = new F();
        f.id = id;
        f.x0 = f.x = f.farX = f.px = x;
        f.y0 = f.y = f.farY = f.py = y;
        f.t0 = SystemClock.uptimeMillis();
        f.left = x < width / 2f;
        f.pts.add(new float[]{x / dens, y / dens});
        if (stroke()) {
            if (fs.size() > 0) {
                f.others = true;
                for (int i = 0; i < fs.size(); i++) fs.valueAt(i).others = true;
            }
        }
        fs.put(id, f);
    }

    void move(int id, float x, float y) {
        F f = fs.get(id);
        if (f == null) return;
        f.len += Math.hypot(x - f.x, y - f.y) / dens;
        f.x = x;
        f.y = y;
        if (stroke()) {
            f.pts.add(new float[]{x / dens, y / dens});
            out.redraw();
            return;
        }
        float d = (float) Math.hypot(x - f.x0, y - f.y0) / dens;
        if (d > f.maxD) {
            f.maxD = d;
            f.farX = x;
            f.farY = y;
        }
        int dir = d >= flickDp ? dirOf(x - f.x0, y - f.y0) : 0;
        // 角から指を離さずに曲げる:
        //   右の ↖（ん）から → 削除 / ↓ 確定 / ↑ Tab
        //   ↖ ↙ どちらの手でも ↙ から ↑ Shift / → Win / ↓ Ctrl（修飾キーの選択中だけ有効。判断はPC）
        if (f.corner == 0 && !f.seIn && ((dir == 8 && !f.left) || dir == 6)) {
            f.corner = dir;
            f.ax = x;
            f.ay = y;
        } else if (f.corner != 0) {
            float sx = x - f.px, sy = y - f.py;
            // 角の方向へ進んでいる間は基準点もついていく（曲げた後は固定）
            if (!f.ulFrozen && Math.hypot(sx, sy) / dens > 0.5f && dirOf(sx, sy) == f.corner) {
                f.ax = x;
                f.ay = y;
            }
            float vx = (x - f.ax) / dens, vy = (y - f.ay) / dens;
            String k = null;
            if (Math.hypot(vx, vy) >= bendDp) {
                int vd = dirOf(vx, vy);
                if (f.corner == 8) k = vd == 3 ? "bs" : vd == 5 ? "enter" : vd == 1 ? "tab" : null;
                else k = vd == 1 ? "mshift" : vd == 3 ? "mwin" : vd == 5 ? "mctrl" : null;
            }
            if (k != null) f.ulFrozen = true;
            boolean changed = k == null ? f.ulKind != null : !k.equals(f.ulKind);
            if (changed) {
                f.ulKind = k;
                if ("bs".equals(k)) startRepeat(f);
                else stopRepeat(f);
                if (k != null) {
                    previewBend(k, f.left ? "L" : "R");
                    out.vibrate(20, 220);
                } else {
                    out.vibrate(8, 120); // 戻した → 取り消し
                }
            }
        }
        // 右の親指: ↘（切り替え口）に入った → 音で知らせる。そこから左へ引いたら英字
        if (!f.left) {
            if (!f.seIn && f.corner == 0 && dir == 4) {
                f.seIn = true;
                f.seX = x;
                preview(4, false);
                out.vibrate(8, 120);
            }
            if (f.seIn) {
                if (x > f.seX && !f.bend) f.seX = x;
                if (!f.bend && (f.seX - x) / dens >= bendDp) {
                    f.bend = true;
                    preview(4, true);
                    out.vibrate(15, 200);
                }
            }
        }
        f.px = x;
        f.py = y;
    }

    void up(int id, float x, float y) {
        move(id, x, y);
        F f = fs.get(id);
        if (f == null) return;
        fs.remove(id);
        stopRepeat(f);
        if (f.repeated) return; // 押さえたまま連続で消した → 離した時には何も送らない
        long now = SystemClock.uptimeMillis();
        if (stroke()) {
            upEnglish(f, now);
            return;
        }
        int dir;
        boolean lng = false;
        String kind = null;
        if (f.bend) {
            dir = 4;
            kind = "bend";
        } else if (f.ulKind != null) {
            dir = f.corner;
            kind = f.ulKind;
        } else if (f.maxD < flickDp) {
            dir = 0;
        } else {
            dir = dirOf(f.farX - f.x0, f.farY - f.y0);
        }
        try {
            JSONObject o = new JSONObject();
            o.put("t", "df");
            o.put("s", f.left ? "L" : "R");
            o.put("n", dir);
            o.put("z", lng ? 1 : 0);
            if (kind != null) o.put("b", kind);
            out.send(o);
        } catch (Exception ignored) {
        }
    }

    private void upEnglish(F f, long now) {
        out.redraw();
        if (f.holder) return; // 大文字のために押さえていた指
        if (f.len < 12) {
            // トン: 2本の指がほぼ同時にトン → 日本語へ戻る
            if (lastTapT1 > 0 && now - lastTapT1 < 400 && Math.abs(f.t0 - lastTapT0) < 220) {
                lastTapT0 = lastTapT1 = -1;
                if (pendingTap != null) handler.removeCallbacks(pendingTap);
                pendingTap = null;
                send1("d2");
            } else {
                lastTapT0 = f.t0;
                lastTapT1 = now;
                // 1本だけのトン（もう1本が続かなければ）: 修飾キーだけを押す用
                if (pendingTap != null) handler.removeCallbacks(pendingTap);
                pendingTap = new Runnable() {
                    public void run() {
                        pendingTap = null;
                        send1("d1");
                    }
                };
                handler.postDelayed(pendingTap, 260);
            }
            return;
        }
        boolean upper = f.others || fs.size() > 0;
        for (int i = 0; i < fs.size(); i++) fs.valueAt(i).holder = true;
        try {
            JSONArray a = new JSONArray();
            int step = Math.max(1, f.pts.size() / 160);
            for (int i = 0; i < f.pts.size(); i += step) {
                float[] p = f.pts.get(i);
                JSONArray q = new JSONArray();
                q.put(Math.round(p[0] * 10) / 10.0);
                q.put(Math.round(p[1] * 10) / 10.0);
                a.put(q);
            }
            float[] last = f.pts.get(f.pts.size() - 1);
            JSONArray q = new JSONArray();
            q.put(Math.round(last[0] * 10) / 10.0);
            q.put(Math.round(last[1] * 10) / 10.0);
            a.put(q);
            JSONObject o = new JSONObject();
            o.put("t", "ds");
            o.put("pts", a);
            o.put("u", upper);
            out.send(o);
        } catch (Exception ignored) {
        }
    }

    private void preview(int dir, boolean bend) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", "dp");
            o.put("s", "R");
            o.put("n", dir);
            if (bend) o.put("b", "bend");
            out.send(o);
        } catch (Exception ignored) {
        }
    }

    private void sendFlick(String side, int dir, String kind) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", "df");
            o.put("s", side);
            o.put("n", dir);
            o.put("z", 0);
            if (kind != null) o.put("b", kind);
            out.send(o);
        } catch (Exception ignored) {
        }
    }

    private void previewBend(String kind, String side) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", "dp");
            o.put("s", side);
            o.put("n", kind.startsWith("m") ? 6 : 8);
            o.put("b", kind);
            out.send(o);
        } catch (Exception ignored) {
        }
    }

    private void send1(String t) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", t);
            out.send(o);
        } catch (Exception ignored) {
        }
    }

    // ===== 描画（見ない前提なので確認用の簡素な表示） =====

    private static final String[] JA_L = {"は", "さ", "た", "ま", "わ", "ら", "や", "な", "か"};
    private static final String[] JA_R = {"あ", "う", "変換", "え", "切替", "お", "゛小", "い", "ん"};
    private static final String[] NUM_L = {"5", "2", "3", "6", "9", "8", "7", "4", "1"};
    private static final String[] NUM_R = {"0", ".", ":", "+", "切替", ",", "%", "-", "/"};
    private static final float[][] OFF = {{0, 0}, {0, -1}, {0.75f, -0.75f}, {1, 0}, {0.75f, 0.75f}, {0, 1}, {-0.75f, 0.75f}, {-1, 0}, {-0.75f, -0.75f}};

    void draw(Canvas c, int w, int h) {
        text.setColor(Color.rgb(150, 150, 165));
        if (stroke()) {
            text.setTextSize(16 * dens);
            c.drawText("en".equals(sub)
                    ? "英字：どこにでも一筆書き（押さえたまま書くと大文字／両手で同時にトン＝日本語）"
                    : "修飾キーの後：1文字を一筆書き（トン＝修飾キーだけ／両手で同時にトン＝取り消し）", w / 2f, 26 * dens, text);
        } else {
            paint.setColor(Color.rgb(40, 40, 48));
            paint.setStrokeWidth(dens);
            c.drawLine(w / 2f, 12 * dens, w / 2f, h - 12 * dens, paint);
            boolean ja = "ja".equals(sub);
            text.setTextSize(13 * dens);
            c.drawText(ja ? "日本語　左：行　右：段" : "数字　左：1〜9　右：0と記号", w / 2f, 22 * dens, text);
            rose(c, w * 0.25f, h * 0.55f, Math.min(w * 0.17f, h * 0.3f), ja ? JA_L : NUM_L);
            rose(c, w * 0.75f, h * 0.55f, Math.min(w * 0.17f, h * 0.3f), ja ? JA_R : NUM_R);
        }
        // 触れている指の軌跡
        paint.setColor(Color.rgb(120, 110, 220));
        paint.setStrokeWidth(4 * dens);
        paint.setStyle(Paint.Style.STROKE);
        paint.setStrokeCap(Paint.Cap.ROUND);
        for (int i = 0; i < fs.size(); i++) {
            F f = fs.valueAt(i);
            if (stroke()) {
                Path p = new Path();
                for (int j = 0; j < f.pts.size(); j++) {
                    float[] q = f.pts.get(j);
                    if (j == 0) p.moveTo(q[0] * dens, q[1] * dens);
                    else p.lineTo(q[0] * dens, q[1] * dens);
                }
                c.drawPath(p, paint);
            } else {
                c.drawLine(f.x0, f.y0, f.x, f.y, paint);
            }
        }
        paint.setStyle(Paint.Style.FILL);
    }

    private void rose(Canvas c, float cx, float cy, float r, String[] labels) {
        text.setTextSize(Math.max(12 * dens, r * 0.28f));
        for (int i = 0; i < 9; i++) {
            float x = cx + OFF[i][0] * r, y = cy + OFF[i][1] * r;
            text.setColor(labels[i].length() > 1 ? Color.rgb(110, 110, 120) : Color.rgb(190, 190, 205));
            c.drawText(labels[i], x, y + text.getTextSize() * 0.35f, text);
        }
    }
}
