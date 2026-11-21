package io.github.charlotte5227.phoneboard;

import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.media.AudioAttributes;
import android.media.SoundPool;
import android.net.wifi.WifiManager;
import android.os.Build;
import android.os.Bundle;
import android.os.VibrationEffect;
import android.os.Vibrator;
import android.text.InputType;
import android.util.TypedValue;
import android.view.KeyEvent;
import android.view.View;
import android.view.Window;
import android.view.WindowManager;
import android.view.inputmethod.InputMethodManager;
import android.widget.Button;
import android.widget.CheckBox;
import android.widget.CompoundButton;
import android.widget.EditText;
import android.widget.FrameLayout;
import android.widget.LinearLayout;
import android.widget.ScrollView;
import android.widget.TextView;

import org.json.JSONArray;
import org.json.JSONObject;

import java.io.File;
import java.io.FileOutputStream;
import java.util.List;
import java.util.Random;

public class MainActivity extends Activity implements Client.Listener, PadView.Host {

    private Client client;
    private PadView pad;
    private ScrollView setup;
    private TextView statusView;
    private LinearLayout foundList;
    private EditText hostEdit, pinEdit;
    private CheckBox dimCheck;
    private Button backButton;
    private SharedPreferences prefs;
    private Vibrator vibrator;
    private boolean setupByUser;
    private String lastStatus = "";
    private WifiManager.WifiLock wifiLock;
    private SoundPool sounds;

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        requestWindowFeature(Window.FEATURE_NO_TITLE);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON | WindowManager.LayoutParams.FLAG_FULLSCREEN);
        if (Build.VERSION.SDK_INT >= 28) {
            WindowManager.LayoutParams lp = getWindow().getAttributes();
            lp.layoutInDisplayCutoutMode = WindowManager.LayoutParams.LAYOUT_IN_DISPLAY_CUTOUT_MODE_SHORT_EDGES;
            getWindow().setAttributes(lp);
        }
        prefs = getSharedPreferences("pb", MODE_PRIVATE);
        vibrator = (Vibrator) getSystemService(Context.VIBRATOR_SERVICE);
        initSounds();

        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        pad = new PadView(this, this);
        root.addView(pad, new FrameLayout.LayoutParams(-1, -1));
        setup = buildSetup();
        root.addView(setup, new FrameLayout.LayoutParams(-1, -1));
        setContentView(root);

        client = new Client(this, this);
        String host = prefs.getString("host", "");
        client.setTarget(host, prefs.getInt("port", Client.DEFAULT_PORT), prefs.getString("pin", ""), prefs.getString("pcName", ""));
        pad.addOnLayoutChangeListener(new View.OnLayoutChangeListener() {
            public void onLayoutChange(View v, int l, int t, int r, int bt, int ol, int ot, int or, int ob) {
                float d = getResources().getDisplayMetrics().density;
                client.setDevice(Build.MANUFACTURER + " " + Build.MODEL, (r - l) / d, (bt - t) / d);
            }
        });
        float d = getResources().getDisplayMetrics().density;
        int sw = getResources().getDisplayMetrics().widthPixels, sh = getResources().getDisplayMetrics().heightPixels;
        client.setDevice(Build.MANUFACTURER + " " + Build.MODEL, Math.max(sw, sh) / d, Math.min(sw, sh) / d);
        client.start();
        showSetup(host.length() == 0);
        if (host.length() == 0) discover();
    }

    @Override
    protected void onDestroy() {
        client.stop();
        if (sounds != null) sounds.release();
        super.onDestroy();
    }

    @Override
    protected void onResume() {
        super.onResume();
        acquireWifiLock();
        hideSystemUi();
        applyBrightness();
    }

    @Override
    protected void onPause() {
        super.onPause();
        if (wifiLock != null && wifiLock.isHeld()) wifiLock.release();
    }

    /** Wi-Fiの省電力（パケットがまとめて遅れて届く）を止めて、カクつき・切断を防ぐ。 */
    private void acquireWifiLock() {
        try {
            if (wifiLock == null) {
                WifiManager wm = (WifiManager) getApplicationContext().getSystemService(Context.WIFI_SERVICE);
                if (wm == null) return;
                int mode = Build.VERSION.SDK_INT >= 29 ? WifiManager.WIFI_MODE_FULL_LOW_LATENCY : WifiManager.WIFI_MODE_FULL_HIGH_PERF;
                wifiLock = wm.createWifiLock(mode, "PhoneBoard");
                wifiLock.setReferenceCounted(false);
            }
            if (!wifiLock.isHeld()) wifiLock.acquire();
        } catch (Throwable ignored) {
        }
    }

    @Override
    public void onWindowFocusChanged(boolean hasFocus) {
        super.onWindowFocusChanged(hasFocus);
        if (hasFocus) hideSystemUi();
    }

    private void hideSystemUi() {
        getWindow().getDecorView().setSystemUiVisibility(View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
                | View.SYSTEM_UI_FLAG_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_FULLSCREEN
                | View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION | View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
                | View.SYSTEM_UI_FLAG_LAYOUT_STABLE);
    }

    // ===== クリック音・注意音（スマホで鳴らす設定の時）。PC側と同じ合成音 =====

    private static final String[] GROUPS = {"vowel", "letter", "symbol", "heavy", "func", "mod", "warn"};
    private final int[] soundIds = new int[GROUPS.length];

    private void initSounds() {
        try {
            sounds = new SoundPool.Builder().setMaxStreams(8).setAudioAttributes(new AudioAttributes.Builder()
                    .setUsage(AudioAttributes.USAGE_ASSISTANCE_SONIFICATION)
                    .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION).build()).build();
            for (int i = 0; i < GROUPS.length; i++) soundIds[i] = sounds.load(writeWav(GROUPS[i]), 1);
        } catch (Throwable ignored) {
            sounds = null;
        }
    }

    private static double sin(double f, double t) { return Math.sin(2 * Math.PI * f * t); }

    private static double synthAt(String g, double t, Random rng) {
        double noise = rng.nextDouble() * 2 - 1;
        if ("vowel".equals(g)) return 0.65 * (sin(1900, t) + 0.3 * sin(3800, t)) * Math.exp(-t / 0.022) + 0.25 * noise * Math.exp(-t / 0.003);
        if ("symbol".equals(g)) {
            double tri = 2 / Math.PI * Math.asin(sin(950, t)) + 0.4 * sin(1900, t);
            return 0.6 * tri * Math.exp(-t / 0.012) + 0.35 * noise * Math.exp(-t / 0.004);
        }
        if ("heavy".equals(g)) return 0.8 * sin(430, t) * Math.exp(-t / 0.028) + 0.45 * noise * Math.exp(-t / 0.007);
        if ("func".equals(g)) {
            double v = 0.5 * sin(1700, t) * Math.exp(-t / 0.006);
            double t2 = t - 0.018;
            if (t2 >= 0) v += 0.5 * sin(1700, t2) * Math.exp(-t2 / 0.006);
            return v + 0.3 * noise * Math.exp(-t / 0.003);
        }
        if ("mod".equals(g)) return 0.45 * sin(3000, t) * Math.exp(-t / 0.004) + 0.2 * noise * Math.exp(-t / 0.002);
        if ("warn".equals(g)) return 0.75 * (tone(1050, t, 0, 0.065) + tone(700, t, 0.090, 0.100));
        return 0.45 * sin(1500, t) * Math.exp(-t / 0.010) + 0.55 * noise * Math.exp(-t / 0.006);
    }

    private static double tone(double f, double t, double t0, double d) {
        double x = t - t0;
        if (x < 0 || x > d) return 0;
        double env = Math.min(1, x / 0.004) * Math.exp(-x / (d * 0.6));
        return (sin(f, x) + 0.25 * sin(3 * f, x)) * env;
    }

    private static double duration(String g) {
        if ("vowel".equals(g)) return 0.055;
        if ("symbol".equals(g)) return 0.040;
        if ("heavy".equals(g)) return 0.070;
        if ("func".equals(g)) return 0.045;
        if ("mod".equals(g)) return 0.022;
        if ("warn".equals(g)) return 0.200;
        return 0.032;
    }

    private String writeWav(String g) throws Exception {
        int rate = 44100;
        int n = (int) (duration(g) * rate);
        Random rng = new Random(g.length() + 7);
        byte[] b = new byte[44 + n * 2];
        System.arraycopy("RIFF".getBytes("US-ASCII"), 0, b, 0, 4);
        le32(b, 4, 36 + n * 2);
        System.arraycopy("WAVEfmt ".getBytes("US-ASCII"), 0, b, 8, 8);
        le32(b, 16, 16);
        le16(b, 20, 1);
        le16(b, 22, 1);
        le32(b, 24, rate);
        le32(b, 28, rate * 2);
        le16(b, 32, 2);
        le16(b, 34, 16);
        System.arraycopy("data".getBytes("US-ASCII"), 0, b, 36, 4);
        le32(b, 40, n * 2);
        for (int i = 0; i < n; i++) {
            double v = Math.max(-1, Math.min(1, synthAt(g, i / (double) rate, rng)));
            le16(b, 44 + i * 2, (int) (v * 32000));
        }
        File f = new File(getCacheDir(), "snd_" + g + ".wav");
        FileOutputStream os = new FileOutputStream(f);
        os.write(b);
        os.close();
        return f.getAbsolutePath();
    }

    private static void le16(byte[] b, int o, int v) {
        b[o] = (byte) v;
        b[o + 1] = (byte) (v >> 8);
    }

    private static void le32(byte[] b, int o, int v) {
        le16(b, o, v);
        le16(b, o + 2, v >> 16);
    }

    /** group: 音の系統、semi: 高さ（半音、0〜12）、vol: 0〜100 */
    private void playSound(String group, int semi, int vol) {
        if (sounds == null) return;
        int idx = 1;
        for (int i = 0; i < GROUPS.length; i++) if (GROUPS[i].equals(group)) idx = i;
        float v = Math.max(0, Math.min(1, vol / 100f));
        float rate = "warn".equals(group) ? 1f : (float) Math.max(0.5, Math.min(2.0, Math.pow(2, semi / 12.0)));
        sounds.play(soundIds[idx], v, v, 1, 0, rate);
    }

    // ===== 接続設定画面 =====

    private int dp(float v) {
        return (int) TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_DIP, v, getResources().getDisplayMetrics());
    }

    private TextView label(LinearLayout parent, String s, float sp, boolean bold) {
        TextView t = new TextView(this);
        t.setText(s);
        t.setTextColor(Color.rgb(230, 230, 236));
        t.setTextSize(sp);
        if (bold) t.setTypeface(Typeface.DEFAULT_BOLD);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, -2);
        lp.topMargin = dp(8);
        parent.addView(t, lp);
        return t;
    }

    private Button button(LinearLayout parent, String s, View.OnClickListener l) {
        Button b = new Button(this);
        b.setText(s);
        b.setAllCaps(false);
        b.setTextSize(17);
        b.setOnClickListener(l);
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, dp(52));
        lp.topMargin = dp(8);
        parent.addView(b, lp);
        return b;
    }

    private EditText edit(LinearLayout parent, String hint, int type) {
        EditText e = new EditText(this);
        e.setHint(hint);
        e.setInputType(type);
        e.setTextColor(Color.WHITE);
        e.setHintTextColor(Color.rgb(120, 120, 130));
        e.setTextSize(20);
        e.setSingleLine(true);
        e.setImeOptions(0x02000000 /* IME_FLAG_NO_EXTRACT_UI: 横向きでも全画面入力にしない */);
        parent.addView(e, new LinearLayout.LayoutParams(-1, -2));
        return e;
    }

    private ScrollView buildSetup() {
        ScrollView sv = new ScrollView(this);
        sv.setBackgroundColor(Color.rgb(21, 22, 27));
        LinearLayout cols = new LinearLayout(this);
        cols.setOrientation(LinearLayout.HORIZONTAL);
        cols.setPadding(dp(24), dp(16), dp(24), dp(24));
        sv.addView(cols);
        // 横向きなので左右2列
        LinearLayout ll = new LinearLayout(this);
        ll.setOrientation(LinearLayout.VERTICAL);
        LinearLayout rr = new LinearLayout(this);
        rr.setOrientation(LinearLayout.VERTICAL);
        LinearLayout.LayoutParams half = new LinearLayout.LayoutParams(0, -2, 1);
        half.rightMargin = dp(20);
        cols.addView(ll, half);
        cols.addView(rr, new LinearLayout.LayoutParams(0, -2, 1));

        label(ll, "PhoneBoard", 26, true);
        statusView = label(ll, "", 15, false);
        statusView.setTextColor(Color.rgb(255, 200, 61));

        label(ll, "1. PCを選ぶ", 17, true);
        button(ll, "同じWi-FiのPCを探す", new View.OnClickListener() {
            public void onClick(View v) { discover(); }
        });
        foundList = new LinearLayout(this);
        foundList.setOrientation(LinearLayout.VERTICAL);
        ll.addView(foundList);
        hostEdit = edit(ll, "PCのIPアドレス（例 192.168.1.10）", InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        hostEdit.setText(prefs.getString("host", ""));

        label(ll, "2. PC画面に表示されたPIN", 17, true);
        pinEdit = edit(ll, "PIN（4桁）", InputType.TYPE_CLASS_NUMBER);
        pinEdit.setText(prefs.getString("pin", ""));

        button(ll, "接続する", new View.OnClickListener() {
            public void onClick(View v) { connectFromForm(); }
        });

        backButton = button(rr, "操作画面に戻る", new View.OnClickListener() {
            public void onClick(View v) { showSetup(false); }
        });

        dimCheck = new CheckBox(this);
        dimCheck.setText("操作中は画面を暗くする（見ないので節電・寝室で眩しくない）");
        dimCheck.setTextColor(Color.rgb(220, 220, 228));
        dimCheck.setTextSize(15);
        dimCheck.setChecked(prefs.getBoolean("dim", true));
        dimCheck.setOnCheckedChangeListener(new CompoundButton.OnCheckedChangeListener() {
            public void onCheckedChanged(CompoundButton b, boolean c) {
                prefs.edit().putBoolean("dim", c).apply();
            }
        });
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, -2);
        lp.topMargin = dp(14);
        rr.addView(dimCheck, lp);

        TextView help = label(rr,
                "使い方（横向き・両手の親指で。スマホは見なくてOK）\n" +
                "・左上の角のボタン / 音量↑ : マウス ⇔ キーボード\n" +
                "・キーボード : 指を置くとPC画面に指の位置とキーが表示されます。離した時に入力（置いたまま滑らせて直せる）。最上段の空いた所で離すと取り消し\n" +
                "・Shift/Ctrl/Alt/Win/Fn : 1回押すと次のキーに効く、2回で固定\n" +
                "・マウス : パッドでカーソル、タップ=左クリック、2本指=スクロール、2本指でトン=右クリック。左端のボタンは押している間ボタンを押しっぱなし（もう片方の指でドラッグ）\n" +
                "・3本指で2秒押さえる / 戻る : この画面\n" +
                "・配置や動作はPCの設定画面（タスクトレイのアイコン）で変更できます",
                14, false);
        help.setTextColor(Color.rgb(160, 162, 178));
        help.setLineSpacing(0, 1.25f);
        return sv;
    }

    private void connectFromForm() {
        String h = hostEdit.getText().toString().trim();
        String p = pinEdit.getText().toString().trim();
        String name = prefs.getString("host", "").equals(h) ? prefs.getString("pcName", "") : (String) hostEdit.getTag();
        prefs.edit().putString("host", h).putString("pin", p).putString("pcName", name == null ? "" : name).apply();
        int port = prefs.getInt("port", Client.DEFAULT_PORT);
        client.setTarget(h, port, p, name == null ? "" : name);
        hideKeyboard();
        setupByUser = false;
    }

    private void hideKeyboard() {
        InputMethodManager imm = (InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE);
        if (imm != null) imm.hideSoftInputFromWindow(setup.getWindowToken(), 0);
    }

    private void discover() {
        foundList.removeAllViews();
        label(foundList, "探しています…", 15, false);
        new Thread(new Runnable() {
            public void run() {
                final List<Client.Found> list = Client.discover(MainActivity.this, 2000);
                runOnUiThread(new Runnable() {
                    public void run() {
                        foundList.removeAllViews();
                        if (list.isEmpty()) {
                            label(foundList, "見つかりませんでした。PCソフトが起動しているか、同じWi-Fiか確認するか、IPアドレスを直接入力してください。", 14, false);
                        }
                        for (final Client.Found f : list) {
                            button(foundList, f.name + "（" + f.host + "）", new View.OnClickListener() {
                                public void onClick(View v) {
                                    hostEdit.setText(f.host);
                                    hostEdit.setTag(f.name);
                                    prefs.edit().putInt("port", f.port).apply();
                                    pinEdit.requestFocus();
                                }
                            });
                        }
                        if (list.size() == 1 && hostEdit.getText().length() == 0) {
                            hostEdit.setText(list.get(0).host);
                            hostEdit.setTag(list.get(0).name);
                            prefs.edit().putInt("port", list.get(0).port).apply();
                        }
                    }
                });
            }
        }).start();
    }

    private void showSetup(boolean show) {
        setup.setVisibility(show ? View.VISIBLE : View.GONE);
        if (!show) hideKeyboard();
        backButton.setVisibility(client != null && client.isConnected() ? View.VISIBLE : View.GONE);
        applyBrightness();
        hideSystemUi();
    }

    private void applyBrightness() {
        WindowManager.LayoutParams lp = getWindow().getAttributes();
        boolean dim = prefs.getBoolean("dim", true) && setup.getVisibility() != View.VISIBLE && client != null && client.isConnected();
        lp.screenBrightness = dim ? 0.01f : WindowManager.LayoutParams.BRIGHTNESS_OVERRIDE_NONE;
        getWindow().setAttributes(lp);
    }

    // ===== Client.Listener =====

    @Override
    public void onStatus(String text, boolean connected) {
        boolean was = lastStatus.startsWith("接続済み");
        lastStatus = text;
        statusView.setText(text);
        pad.setConnected(connected, text);
        if (connected && !was) {
            vibratePattern(new long[]{0, 25, 70, 25}, 220);
            if (!setupByUser) showSetup(false);
        } else if (!connected && was) {
            vibrate(300, 255);
        }
        // 未接続の間は接続設定画面を出しておく（つながれば自動で消える）
        if (!connected && setup.getVisibility() != View.VISIBLE) {
            setupByUser = false;
            showSetup(true);
        }
        backButton.setVisibility(connected ? View.VISIBLE : View.GONE);
        applyBrightness();
    }

    @Override
    public void onDenied() {
        setupByUser = true;
        showSetup(true);
        pinEdit.requestFocus();
    }

    @Override
    public void onMessage(JSONObject m) {
        String t = m.optString("t");
        if ("cfg".equals(t)) {
            pad.applyCfg(m);
        } else if ("setmode".equals(t)) {
            pad.setMode(m.optString("m", "mouse"));
        } else if ("state".equals(t)) {
            pad.setState(m);
        } else if ("click".equals(t)) {
            playSound(m.optString("g", "letter"), m.optInt("p", 6), m.optInt("vol", 60));
        } else if ("vib".equals(t)) {
            int amp = m.optInt("amp", 200);
            JSONArray pat = m.optJSONArray("pat");
            if (pat != null) {
                long[] p = new long[pat.length()];
                for (int i = 0; i < p.length; i++) p[i] = pat.optLong(i);
                vibratePattern(p, amp);
            } else {
                vibrate(m.optLong("ms", 10), amp);
            }
        }
    }

    // ===== PadView.Host =====

    @Override
    public void send(JSONObject o) {
        client.send(o);
    }

    @Override
    public void sendFast(JSONObject o) {
        client.sendFast(o);
    }

    @Override
    public void openSetup() {
        setupByUser = true;
        showSetup(true);
    }

    @Override
    public void vibrate(long ms, int amp) {
        if (vibrator == null || ms <= 0) return;
        try {
            if (Build.VERSION.SDK_INT >= 26) {
                vibrator.vibrate(VibrationEffect.createOneShot(ms, Math.max(1, Math.min(255, amp))));
            } else {
                vibrator.vibrate(ms);
            }
        } catch (Throwable ignored) {
        }
    }

    @Override
    public void vibratePattern(long[] pattern, int amp) {
        if (vibrator == null || pattern.length == 0) return;
        try {
            if (Build.VERSION.SDK_INT >= 26) {
                int[] amps = new int[pattern.length];
                for (int i = 0; i < amps.length; i++) amps[i] = (i % 2 == 1) ? Math.max(1, Math.min(255, amp)) : 0;
                vibrator.vibrate(VibrationEffect.createWaveform(pattern, amps, -1));
            } else {
                vibrator.vibrate(pattern, -1);
            }
        } catch (Throwable ignored) {
        }
    }

    // ===== 音量キー・戻るキー =====

    @Override
    public boolean dispatchKeyEvent(KeyEvent e) {
        int code = e.getKeyCode();
        boolean vol = code == KeyEvent.KEYCODE_VOLUME_UP || code == KeyEvent.KEYCODE_VOLUME_DOWN;
        if (vol && client.isConnected() && setup.getVisibility() != View.VISIBLE) {
            if (e.getRepeatCount() == 0 && (e.getAction() == KeyEvent.ACTION_DOWN || e.getAction() == KeyEvent.ACTION_UP)) {
                try {
                    JSONObject o = new JSONObject();
                    o.put("t", "hw");
                    o.put("k", code == KeyEvent.KEYCODE_VOLUME_UP ? "volup" : "voldown");
                    o.put("a", e.getAction() == KeyEvent.ACTION_DOWN ? "down" : "up");
                    client.send(o);
                } catch (Exception ignored) {
                }
            }
            return true;
        }
        if (code == KeyEvent.KEYCODE_BACK) {
            // 誤操作でアプリが閉じないように: 操作画面では戻る＝設定画面、設定画面では（接続中なら）操作画面へ
            if (setup.getVisibility() != View.VISIBLE) {
                if (e.getAction() == KeyEvent.ACTION_UP) openSetup();
                return true;
            }
            if (client.isConnected()) {
                if (e.getAction() == KeyEvent.ACTION_UP) showSetup(false);
                return true;
            }
        }
        return super.dispatchKeyEvent(e);
    }
}
