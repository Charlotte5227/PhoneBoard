package io.github.charlotte5227.phoneboard;

import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.Path;
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
        int longHint = -1;           // 「長く引く」を知らせた方向
        final List<float[]> pts = new ArrayList<float[]>();
    }

    private final Out out;
    private final float dens;
    String sub = "ja";
    float flickDp = 16, longDp = 150, bendDp = 30;
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
        longDp = (float) d.optDouble("longDp", longDp);
        bendDp = (float) d.optDouble("bendDp", bendDp);
    }

    void setSub(String m) {
        sub = m;
        fs.clear();
        out.redraw();
    }

    void cancelAll() { fs.clear(); }

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
        f.x0 = f.x = f.farX = x;
        f.y0 = f.y = f.farY = y;
        f.t0 = SystemClock.uptimeMillis();
        f.left = x < width / 2f;
        f.pts.add(new float[]{x / dens, y / dens});
        if ("en".equals(sub)) {
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
        if ("en".equals(sub)) {
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
        // 右の親指: ↘（切り替え口）に入った → 音で知らせる。そこから左へ引いたら英字
        if (!f.left) {
            int dir = d >= flickDp ? dirOf(x - f.x0, y - f.y0) : 0;
            // ← / ↓ / → を「長く引く」の長さまで引いた → 音で知らせる（離す前に戻せば普通の文字）
            if (d >= longDp && (dir == 7 || dir == 5 || dir == 3) && f.longHint != dir) {
                f.longHint = dir;
                previewLong(dir);
                out.vibrate(20, 220);
            } else if (d < longDp * 0.85f && f.longHint >= 0) {
                f.longHint = -1;
                out.vibrate(8, 120);
            }
            if (!f.seIn && dir == 4) {
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
    }

    void up(int id, float x, float y) {
        move(id, x, y);
        F f = fs.get(id);
        if (f == null) return;
        fs.remove(id);
        long now = SystemClock.uptimeMillis();
        if ("en".equals(sub)) {
            upEnglish(f, now);
            return;
        }
        int dir;
        boolean lng = false;
        if (f.bend) {
            dir = 4;
        } else if (f.maxD < flickDp) {
            dir = 0;
        } else {
            dir = dirOf(f.farX - f.x0, f.farY - f.y0);
            // 離した位置が十分遠い時だけ「長く引く」（引きすぎたら戻して離せば普通のはじき）
            lng = Math.hypot(f.x - f.x0, f.y - f.y0) / dens >= longDp;
        }
        try {
            JSONObject o = new JSONObject();
            o.put("t", "df");
            o.put("s", f.left ? "L" : "R");
            o.put("n", dir);
            o.put("z", lng ? 1 : 0);
            if (f.bend) o.put("b", "bend");
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
                send1("d2");
            } else {
                lastTapT0 = f.t0;
                lastTapT1 = now;
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

    private void previewLong(int dir) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", "dp");
            o.put("s", "R");
            o.put("n", dir);
            o.put("z", 1);
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
        if ("en".equals(sub)) {
            text.setTextSize(16 * dens);
            c.drawText("英字：どこにでも一筆書き（押さえたまま書くと大文字／両手で同時にトン＝日本語）", w / 2f, 26 * dens, text);
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
            if ("en".equals(sub)) {
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
