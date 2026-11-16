package io.github.charlotte5227.phoneboard;

import android.app.Activity;
import android.content.Context;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.os.Build;
import android.os.Bundle;
import android.content.Intent;
import android.media.projection.MediaProjectionManager;
import android.provider.Settings;
import android.view.WindowInsets;
import android.os.Vibrator;
import android.net.wifi.WifiManager;
import android.text.InputType;
import android.util.TypedValue;
import android.view.Gravity;
import android.view.KeyEvent;
import android.view.View;
import android.view.ViewGroup;
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

import java.lang.reflect.Method;
import java.util.List;

public class MainActivity extends Activity implements Client.Listener, PadView.Host {

    private Client client;
    private PadView pad;
    private CaptureEdit capture;
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
    private static final int REQ_CAPTURE = 77;
    private boolean mirrorWanted;      // PC設定: キーボード画面を映す
    private boolean captureAsked;      // このセッションで許可を求めたか
    private Button captureButton;
    private final android.os.Handler ui = new android.os.Handler();
    private final Runnable imePoll = new Runnable() {
        public void run() {
            updateMirrorState();
            ui.postDelayed(this, 250);
        }
    };

    @Override
    protected void onCreate(Bundle b) {
        super.onCreate(b);
        requestWindowFeature(Window.FEATURE_NO_TITLE);
        getWindow().addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON | WindowManager.LayoutParams.FLAG_FULLSCREEN);
        if (Build.VERSION.SDK_INT >= 28) {
            try {
                WindowManager.LayoutParams lp = getWindow().getAttributes();
                lp.getClass().getField("layoutInDisplayCutoutMode").setInt(lp, 1); // SHORT_EDGES
                getWindow().setAttributes(lp);
            } catch (Throwable ignored) {
            }
        }
        prefs = getSharedPreferences("pb", MODE_PRIVATE);
        vibrator = (Vibrator) getSystemService(Context.VIBRATOR_SERVICE);

        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        pad = new PadView(this, this);
        root.addView(pad, new FrameLayout.LayoutParams(-1, -1));
        // Gboard等の入力を受け取る見えない入力欄（1px）
        capture = new CaptureEdit(this);
        capture.setSink(new CaptureEdit.Sink() {
            public void onComposing(String text) { sendText("comp", text); }
            public void onCommit(String text) { sendText("text", text); }
            public void onKey(String name) {
                try {
                    JSONObject o = new JSONObject();
                    o.put("t", "key");
                    o.put("k", name);
                    client.send(o);
                } catch (Exception ignored) {
                }
            }
            public void onBack() {
                // キーボードを閉じたらマウスに戻る
                try {
                    JSONObject o = new JSONObject();
                    o.put("t", "act");
                    o.put("a", "{MOUSE}");
                    client.send(o);
                } catch (Exception ignored) {
                }
            }
        });
        root.addView(capture, new FrameLayout.LayoutParams(1, 1));
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
        client.setDevice(Build.MANUFACTURER + " " + Build.MODEL,
                getResources().getDisplayMetrics().widthPixels / d, getResources().getDisplayMetrics().heightPixels / d);
        client.start();
        CaptureService.client = client;
        CaptureService.onStopped = new Runnable() {
            public void run() {
                runOnUiThread(new Runnable() {
                    public void run() { updateCaptureButton(); }
                });
            }
        };
        ui.post(imePoll);
        showSetup(host.length() == 0);
        if (host.length() == 0) discover();
    }

    @Override
    protected void onDestroy() {
        ui.removeCallbacks(imePoll);
        stopService(new Intent(this, CaptureService.class));
        client.stop();
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

    /** Wi-Fiの省電力（パケットがまとめて遅れて届く）を止めて、カーソルのカクつき・切断を防ぐ。 */
    private void acquireWifiLock() {
        try {
            if (wifiLock == null) {
                WifiManager wm = (WifiManager) getApplicationContext().getSystemService(Context.WIFI_SERVICE);
                if (wm == null) return;
                int mode = Build.VERSION.SDK_INT >= 29 ? 4 /* FULL_LOW_LATENCY */ : 3 /* FULL_HIGH_PERF */;
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
        getWindow().getDecorView().setSystemUiVisibility(0x1000 | 0x2 | 0x4 | 0x200 | 0x400 | 0x100);
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
        LinearLayout.LayoutParams lp = new LinearLayout.LayoutParams(-1, dp(54));
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
        parent.addView(e, new LinearLayout.LayoutParams(-1, -2));
        return e;
    }

    private ScrollView buildSetup() {
        ScrollView sv = new ScrollView(this);
        sv.setBackgroundColor(Color.rgb(21, 22, 27));
        LinearLayout ll = new LinearLayout(this);
        ll.setOrientation(LinearLayout.VERTICAL);
        ll.setPadding(dp(22), dp(28), dp(22), dp(40));
        sv.addView(ll);

        label(ll, "PhoneBoard", 28, true);
        statusView = label(ll, "", 16, false);
        statusView.setTextColor(Color.rgb(255, 200, 61));

        label(ll, "1. PCを選ぶ", 18, true);
        button(ll, "同じWi-FiのPCを探す", new View.OnClickListener() {
            public void onClick(View v) { discover(); }
        });
        foundList = new LinearLayout(this);
        foundList.setOrientation(LinearLayout.VERTICAL);
        ll.addView(foundList);
        hostEdit = edit(ll, "PCのIPアドレス（例 192.168.1.10）", InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        hostEdit.setText(prefs.getString("host", ""));

        label(ll, "2. PC画面に表示されたPIN", 18, true);
        pinEdit = edit(ll, "PIN（4桁）", InputType.TYPE_CLASS_NUMBER);
        pinEdit.setText(prefs.getString("pin", ""));

        button(ll, "接続する", new View.OnClickListener() {
            public void onClick(View v) { connectFromForm(); }
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
        ll.addView(dimCheck, lp);

        label(ll, "3. キーボード画面をPCに映す（任意）", 18, true);
        TextView mh = label(ll, "Gboardの画面をPCに映します。許可の画面では「画面全体」を選んでください。"
                + "開発者向けオプションの「タップを表示」をオンにすると、指の位置も映ります。", 14, false);
        mh.setTextColor(Color.rgb(160, 162, 178));
        captureButton = button(ll, "画面の共有を許可する", new View.OnClickListener() {
            public void onClick(View v) {
                if (CaptureService.running) stopService(new Intent(MainActivity.this, CaptureService.class));
                else requestCapture();
                ui.postDelayed(new Runnable() { public void run() { updateCaptureButton(); } }, 300);
            }
        });
        button(ll, "「タップを表示」の設定を開く（開発者向けオプション）", new View.OnClickListener() {
            public void onClick(View v) {
                try {
                    startActivity(new Intent(Settings.ACTION_APPLICATION_DEVELOPMENT_SETTINGS));
                } catch (Exception e) {
                    statusView.setText("開発者向けオプションが無効です。設定→デバイス情報→ビルド番号を7回タップで有効になります");
                }
            }
        });

        backButton = button(ll, "操作画面に戻る", new View.OnClickListener() {
            public void onClick(View v) { showSetup(false); }
        });

        TextView help = label(ll,
                "使い方（スマホは見ずに親指1本で）\n" +
                "・音量↑ : マウス ⇔ キーボード\n" +
                "・マウス : なぞる=カーソル / タップ=左クリック / 右端の帯=スクロール / 下段=キーボード・左右切替・右クリック。音量↓を押している間は左ボタン押しっぱなし\n" +
                "・ダブルタップしてそのままなぞる=ドラッグ\n" +
                "・キーボード : スマホのキーボード（Gboard等）がそのまま開きます。変換中の文字はPC画面に表示され、確定するとPCに入力されます。キーボードを閉じる（戻る）とマウスに戻ります\n" +
                "・画面の左上隅を2秒長押し : この画面\n" +
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
        final TextView t = label(foundList, "探しています…", 15, false);
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
        if (capture != null && pad != null && client != null) updatePhoneKeyboard();
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

    private void requestCapture() {
        captureAsked = true;
        try {
            MediaProjectionManager mpm = (MediaProjectionManager) getSystemService(Context.MEDIA_PROJECTION_SERVICE);
            startActivityForResult(mpm.createScreenCaptureIntent(), REQ_CAPTURE);
        } catch (Exception ignored) {
        }
    }

    @Override
    protected void onActivityResult(int req, int res, Intent data) {
        super.onActivityResult(req, res, data);
        if (req != REQ_CAPTURE) return;
        if (res == RESULT_OK && data != null) {
            Intent i = new Intent(this, CaptureService.class);
            i.putExtra(CaptureService.EXTRA_CODE, res);
            i.putExtra(CaptureService.EXTRA_DATA, data);
            if (Build.VERSION.SDK_INT >= 26) startForegroundService(i);
            else startService(i);
        }
        ui.postDelayed(new Runnable() { public void run() { updateCaptureButton(); } }, 500);
        hideSystemUi();
    }

    private void updateCaptureButton() {
        if (captureButton != null) {
            captureButton.setText(CaptureService.running ? "画面の共有を止める（共有中）" : "画面の共有を許可する");
        }
    }

    /** キーボード（IME）の高さを調べて、映す範囲と送信のON/OFFを決める */
    private void updateMirrorState() {
        boolean on = mirrorWanted && pad.usesPhoneKeyboard() && client.isConnected() && setup.getVisibility() != View.VISIBLE;
        if (on) {
            float top = 0.55f;
            int imeH = 0;
            View decor = getWindow().getDecorView();
            if (Build.VERSION.SDK_INT >= 30) {
                WindowInsets wi = decor.getRootWindowInsets();
                if (wi != null) imeH = wi.getInsets(WindowInsets.Type.ime()).bottom;
            }
            int full = getResources().getDisplayMetrics().heightPixels;
            DisplayMetricsHolder.real(this);
            if (DisplayMetricsHolder.h > 0) full = DisplayMetricsHolder.h;
            if (imeH > 0 && full > 0) top = Math.max(0.05f, (full - imeH) / (float) full - 0.01f);
            CaptureService.cropTop = top;
        }
        CaptureService.active = on;
    }

    static class DisplayMetricsHolder {
        static int h;
        static void real(Activity a) {
            if (h > 0) return;
            android.util.DisplayMetrics dm = new android.util.DisplayMetrics();
            a.getWindowManager().getDefaultDisplay().getRealMetrics(dm);
            h = dm.heightPixels;
        }
    }

    private void sendText(String t, String text) {
        try {
            JSONObject o = new JSONObject();
            o.put("t", t);
            o.put("text", text);
            client.send(o);
        } catch (Exception ignored) {
        }
    }

    /** モードに合わせてスマホのキーボード（Gboard等）を出す/しまう */
    private void updatePhoneKeyboard() {
        final InputMethodManager imm = (InputMethodManager) getSystemService(Context.INPUT_METHOD_SERVICE);
        if (imm == null) return;
        boolean want = pad.usesPhoneKeyboard() && client.isConnected() && setup.getVisibility() != View.VISIBLE;
        if (want) {
            capture.setFocusable(true);
            capture.setFocusableInTouchMode(true);
            capture.requestFocus();
            Runnable show = new Runnable() {
                public void run() {
                    if (!pad.usesPhoneKeyboard()) return;
                    capture.requestFocus();
                    imm.showSoftInput(capture, 0);
                }
            };
            capture.postDelayed(show, 120);
            capture.postDelayed(show, 500); // 1回目で開かない端末向け
        } else if (capture.hasFocus()) {
            imm.hideSoftInputFromWindow(capture.getWindowToken(), 0);
            capture.clearFocus();
            capture.setFocusable(false);
        }
    }

    // ===== Client.Listener =====

    @Override
    public void onStatus(String text, boolean connected) {
        boolean was = pad != null && lastStatus.startsWith("接続済み");
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
            JSONObject kb = m.optJSONObject("kb");
            if (kb != null) {
                mirrorWanted = kb.optBoolean("mirror", false) && "gboard".equals(kb.optString("input"));
                CaptureService.fps = kb.optInt("mirrorFps", 15);
                CaptureService.width = kb.optInt("mirrorWidth", 540);
                CaptureService.quality = kb.optInt("mirrorQuality", 60);
                // 映す設定なのに未許可なら、接続直後（まだスマホを見ている時）に1回だけ許可を求める
                if (mirrorWanted && !CaptureService.running && !captureAsked) requestCapture();
            }
            updatePhoneKeyboard();
        } else if ("setmode".equals(t)) {
            pad.setMode(m.optString("m", "mouse"));
            updatePhoneKeyboard();
        } else if ("state".equals(t)) {
            pad.setState(m);
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
    public void openSetup() {
        setupByUser = true;
        showSetup(true);
    }

    @Override
    public void vibrate(long ms, int amp) {
        if (vibrator == null || ms <= 0) return;
        try {
            if (Build.VERSION.SDK_INT >= 26) {
                Class<?> ve = Class.forName("android.os.VibrationEffect");
                Object eff = ve.getMethod("createOneShot", long.class, int.class).invoke(null, ms, Math.max(1, Math.min(255, amp)));
                Method m = Vibrator.class.getMethod("vibrate", ve);
                m.invoke(vibrator, eff);
            } else {
                vibrator.vibrate(ms);
            }
        } catch (Throwable e) {
            try { vibrator.vibrate(ms); } catch (Throwable ignored) { }
        }
    }

    @Override
    public void vibratePattern(long[] pattern, int amp) {
        if (vibrator == null || pattern.length == 0) return;
        try {
            if (Build.VERSION.SDK_INT >= 26) {
                int[] amps = new int[pattern.length];
                for (int i = 0; i < amps.length; i++) amps[i] = (i % 2 == 1) ? Math.max(1, Math.min(255, amp)) : 0;
                Class<?> ve = Class.forName("android.os.VibrationEffect");
                Object eff = ve.getMethod("createWaveform", long[].class, int[].class, int.class).invoke(null, pattern, amps, -1);
                Vibrator.class.getMethod("vibrate", ve).invoke(vibrator, eff);
            } else {
                vibrator.vibrate(pattern, -1);
            }
        } catch (Throwable e) {
            try { vibrator.vibrate(pattern, -1); } catch (Throwable ignored) { }
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
