package io.github.charlotte5227.phoneboard;

import android.app.Activity;
import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.Service;
import android.content.Context;
import android.content.Intent;
import android.content.pm.ServiceInfo;
import android.graphics.Bitmap;
import android.graphics.PixelFormat;
import android.hardware.display.DisplayManager;
import android.hardware.display.VirtualDisplay;
import android.media.Image;
import android.media.ImageReader;
import android.media.projection.MediaProjection;
import android.media.projection.MediaProjectionManager;
import android.os.Build;
import android.os.Handler;
import android.os.HandlerThread;
import android.os.IBinder;
import android.os.SystemClock;
import android.util.DisplayMetrics;
import android.view.WindowManager;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;

/**
 * 画面キャプチャ（MediaProjection）でスマホ画面の下側＝キーボード部分を切り出し、
 * JPEGにしてPCへ送る。Android 10以降は画面キャプチャを foreground service で行う決まり。
 */
public class CaptureService extends Service {

    public static final String EXTRA_CODE = "code";
    public static final String EXTRA_DATA = "data";

    /** MainActivity と共有する状態 */
    public static volatile Client client;
    public static volatile boolean active;        // 送信するか（キーボード表示中のみ）
    public static volatile float cropTop = 0.55f;  // 画面のこの割合より下を送る
    public static volatile int fps = 15;
    public static volatile int width = 540;
    public static volatile int quality = 60;
    public static volatile boolean running;
    public static volatile Runnable onStopped;

    private MediaProjection projection;
    private VirtualDisplay display;
    private ImageReader reader;
    private HandlerThread thread;
    private Handler handler;
    private long lastSent;
    private Bitmap bmp;
    private final ByteArrayOutputStream jpg = new ByteArrayOutputStream(64 * 1024);

    @Override
    public IBinder onBind(Intent intent) { return null; }

    @Override
    public int onStartCommand(Intent intent, int flags, int startId) {
        startAsForeground();
        if (intent == null || projection != null) return START_NOT_STICKY;
        int code = intent.getIntExtra(EXTRA_CODE, 0);
        Intent data = intent.getParcelableExtra(EXTRA_DATA);
        if (code != Activity.RESULT_OK || data == null) {
            stopSelf();
            return START_NOT_STICKY;
        }
        try {
            MediaProjectionManager mpm = (MediaProjectionManager) getSystemService(Context.MEDIA_PROJECTION_SERVICE);
            projection = mpm.getMediaProjection(code, data);
            startCapture();
            running = true;
        } catch (Throwable e) {
            stopSelf();
        }
        return START_NOT_STICKY;
    }

    private void startAsForeground() {
        String ch = "capture";
        Notification.Builder b;
        if (Build.VERSION.SDK_INT >= 26) {
            NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
            if (nm != null) nm.createNotificationChannel(new NotificationChannel(ch, "キーボード画面の共有", NotificationManager.IMPORTANCE_LOW));
            b = new Notification.Builder(this, ch);
        } else {
            b = new Notification.Builder(this);
        }
        Notification n = b.setContentTitle("PhoneBoard")
                .setContentText("キーボード画面をPCに映しています")
                .setSmallIcon(R.mipmap.ic_launcher)
                .setOngoing(true)
                .build();
        if (Build.VERSION.SDK_INT >= 29) {
            startForeground(1, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PROJECTION);
        } else {
            startForeground(1, n);
        }
    }

    private void startCapture() {
        thread = new HandlerThread("pb-capture");
        thread.start();
        handler = new Handler(thread.getLooper());

        DisplayMetrics dm = new DisplayMetrics();
        WindowManager wm = (WindowManager) getSystemService(Context.WINDOW_SERVICE);
        wm.getDefaultDisplay().getRealMetrics(dm);
        int w = Math.min(Math.max(240, width), dm.widthPixels);
        int h = Math.round(dm.heightPixels * (w / (float) dm.widthPixels));
        int dpi = Math.max(80, Math.round(dm.densityDpi * (w / (float) dm.widthPixels)));

        // Android 14以降はキャプチャ開始前にコールバック登録が必須
        projection.registerCallback(new MediaProjection.Callback() {
            @Override
            public void onStop() { stopSelf(); }
        }, handler);

        reader = ImageReader.newInstance(w, h, PixelFormat.RGBA_8888, 2);
        reader.setOnImageAvailableListener(new ImageReader.OnImageAvailableListener() {
            public void onImageAvailable(ImageReader r) { onFrame(r); }
        }, handler);
        display = projection.createVirtualDisplay("PhoneBoard", w, h, dpi,
                DisplayManager.VIRTUAL_DISPLAY_FLAG_AUTO_MIRROR, reader.getSurface(), null, handler);
    }

    private void onFrame(ImageReader r) {
        Image img = null;
        try {
            img = r.acquireLatestImage();
            if (img == null) return;
            Client c = client;
            long now = SystemClock.uptimeMillis();
            if (!active || c == null || !c.isConnected() || now - lastSent < 1000 / Math.max(1, fps)) return;
            lastSent = now;
            Image.Plane p = img.getPlanes()[0];
            int w = img.getWidth(), h = img.getHeight();
            int rowPx = p.getRowStride() / p.getPixelStride();
            if (bmp == null || bmp.getWidth() != rowPx || bmp.getHeight() != h) {
                bmp = Bitmap.createBitmap(rowPx, h, Bitmap.Config.ARGB_8888);
            }
            ByteBuffer buf = p.getBuffer();
            buf.rewind();
            bmp.copyPixelsFromBuffer(buf);
            int top = Math.max(0, Math.min(h - 16, Math.round(h * cropTop)));
            Bitmap part = Bitmap.createBitmap(bmp, 0, top, w, h - top);
            jpg.reset();
            part.compress(Bitmap.CompressFormat.JPEG, Math.max(20, Math.min(95, quality)), jpg);
            part.recycle();
            c.sendFrame(jpg.toByteArray(), w, h - top);
        } catch (Throwable ignored) {
        } finally {
            if (img != null) img.close();
        }
    }

    @Override
    public void onDestroy() {
        running = false;
        active = false;
        try { if (display != null) display.release(); } catch (Throwable ignored) { }
        try { if (reader != null) reader.close(); } catch (Throwable ignored) { }
        try { if (projection != null) projection.stop(); } catch (Throwable ignored) { }
        if (thread != null) thread.quitSafely();
        projection = null;
        Runnable r = onStopped;
        if (r != null) r.run();
        super.onDestroy();
    }
}
