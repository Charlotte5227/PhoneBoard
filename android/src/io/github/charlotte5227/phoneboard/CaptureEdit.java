package io.github.charlotte5227.phoneboard;

import android.content.Context;
import android.text.Editable;
import android.text.InputType;
import android.view.KeyEvent;
import android.view.inputmethod.EditorInfo;
import android.view.inputmethod.InputConnection;
import android.view.inputmethod.InputConnectionWrapper;
import android.widget.EditText;

/**
 * Gboard など、スマホにインストールされているキーボードの入力を受け取ってPCへ送るための入力欄。
 * 文字は欄に溜めず、確定したらすぐPCへ送って欄を空にする。
 *  - 変換中の文字（setComposingText）   → onComposing（PC画面に表示）
 *  - 確定（commitText / finishComposing）→ onCommit（PCへ入力）
 *  - 空欄での削除・改行・矢印など        → onKey（PCへキー入力）
 */
public class CaptureEdit extends EditText {

    public interface Sink {
        void onComposing(String text);
        void onCommit(String text);
        void onKey(String name);
        void onBack();
    }

    private Sink sink;
    private String comp = "";

    public CaptureEdit(Context c) {
        super(c);
        setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE);
        setImeOptions(EditorInfo.IME_FLAG_NO_EXTRACT_UI | EditorInfo.IME_FLAG_NO_FULLSCREEN);
        setBackgroundColor(0);
        setTextColor(0);
        setCursorVisible(false);
    }

    public void setSink(Sink s) { sink = s; }

    @Override
    public InputConnection onCreateInputConnection(EditorInfo out) {
        InputConnection ic = super.onCreateInputConnection(out);
        out.inputType = InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_FLAG_MULTI_LINE;
        out.imeOptions = EditorInfo.IME_FLAG_NO_EXTRACT_UI | EditorInfo.IME_FLAG_NO_FULLSCREEN | EditorInfo.IME_ACTION_NONE;
        if (ic == null) return null;
        return new Wrap(ic);
    }

    @Override
    public boolean onKeyPreIme(int keyCode, KeyEvent event) {
        // キーボードを閉じる「戻る」→ マウスに戻る
        if (keyCode == KeyEvent.KEYCODE_BACK && event.getAction() == KeyEvent.ACTION_UP && sink != null) {
            sink.onBack();
        }
        return super.onKeyPreIme(keyCode, event);
    }

    private void setComp(String s) {
        if (s.equals(comp)) return;
        comp = s;
        if (sink != null) sink.onComposing(s);
    }

    private static String keyName(int code) {
        switch (code) {
            case KeyEvent.KEYCODE_DEL: return "BS";
            case KeyEvent.KEYCODE_FORWARD_DEL: return "DEL";
            case KeyEvent.KEYCODE_ENTER:
            case KeyEvent.KEYCODE_NUMPAD_ENTER: return "ENTER";
            case KeyEvent.KEYCODE_TAB: return "TAB";
            case KeyEvent.KEYCODE_ESCAPE: return "ESC";
            case KeyEvent.KEYCODE_DPAD_LEFT: return "LEFT";
            case KeyEvent.KEYCODE_DPAD_RIGHT: return "RIGHT";
            case KeyEvent.KEYCODE_DPAD_UP: return "UP";
            case KeyEvent.KEYCODE_DPAD_DOWN: return "DOWN";
            case KeyEvent.KEYCODE_MOVE_HOME: return "HOME";
            case KeyEvent.KEYCODE_MOVE_END: return "END";
            case KeyEvent.KEYCODE_SPACE: return "SPACE";
        }
        return null;
    }

    private class Wrap extends InputConnectionWrapper {
        Wrap(InputConnection target) { super(target, true); }

        private void clearField() {
            Editable e = getText();
            if (e != null && e.length() > 0) e.clear();
        }

        @Override
        public boolean setComposingText(CharSequence text, int newCursorPosition) {
            setComp(text == null ? "" : text.toString());
            return super.setComposingText(text, newCursorPosition);
        }

        @Override
        public boolean commitText(CharSequence text, int newCursorPosition) {
            String t = text == null ? "" : text.toString();
            setComp("");
            super.setComposingText("", 1);
            super.finishComposingText();
            clearField();
            if (t.length() > 0 && sink != null) sink.onCommit(t);
            return true;
        }

        @Override
        public boolean finishComposingText() {
            String t = comp;
            setComp("");
            super.setComposingText("", 1);
            boolean r = super.finishComposingText();
            clearField();
            if (t.length() > 0 && sink != null) sink.onCommit(t);
            return r;
        }

        @Override
        public boolean deleteSurroundingText(int beforeLength, int afterLength) {
            if (comp.length() == 0 && sink != null) {
                // 欄は常に空なので、削除はそのままPCへ
                for (int i = 0; i < Math.min(beforeLength, 50); i++) sink.onKey("BS");
                for (int i = 0; i < Math.min(afterLength, 50); i++) sink.onKey("DEL");
                return true;
            }
            return super.deleteSurroundingText(beforeLength, afterLength);
        }

        @Override
        public boolean sendKeyEvent(KeyEvent event) {
            if (comp.length() == 0 && sink != null) {
                String k = keyName(event.getKeyCode());
                if (k != null) {
                    if (event.getAction() == KeyEvent.ACTION_DOWN) sink.onKey(k);
                    return true;
                }
                int ch = event.getUnicodeChar();
                if (ch > 0) {
                    if (event.getAction() == KeyEvent.ACTION_DOWN) sink.onCommit(new String(Character.toChars(ch)));
                    return true;
                }
            }
            return super.sendKeyEvent(event);
        }
    }
}
