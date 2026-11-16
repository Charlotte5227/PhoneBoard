#!/bin/sh
# PhoneBoard Android APK ビルド（Gradle不要。Ubuntu/Debian の android-sdk パッケージを使用）
#   sudo apt install aapt apksigner zipalign dalvik-exchange default-jdk-headless
set -e
cd "$(dirname "$0")"
# API 29以上の android.jar が必要（画面共有サービスの宣言のため）。
# 優先順: $ANDROID_JAR → $ANDROID_HOME/platforms の最新
PLAT=${ANDROID_JAR:-}
if [ -z "$PLAT" ] && [ -n "$ANDROID_HOME" ]; then
  PLAT=$(ls -d "$ANDROID_HOME"/platforms/android-3[0-9]/android.jar 2>/dev/null | sort -V | tail -1)
fi
if [ -z "$PLAT" ] || [ ! -f "$PLAT" ]; then
  echo "android.jar (API 30以上) が見つかりません。ANDROID_JAR か ANDROID_HOME を設定してください" >&2; exit 1
fi
OUT=${1:-../dist/PhoneBoard.apk}
rm -rf build && mkdir -p build/gen build/obj build/apk "$(dirname "$OUT")"
aapt package -f -m -J build/gen -M AndroidManifest.xml -S res -I "$PLAT"
javac -nowarn -encoding UTF-8 -source 8 -target 8 -bootclasspath "$PLAT" -d build/obj \
  $(find src build/gen -name '*.java') 2>&1 | grep -v -e 'bootstrap class path' -e '^warning' -e '^1 warning' -e '^4 warnings' || true
dalvik-exchange --dex --min-sdk-version=21 --output=build/apk/classes.dex build/obj
aapt package -f -M AndroidManifest.xml -S res -I "$PLAT" -F build/unsigned.apk build/apk
zipalign -f 4 build/unsigned.apk build/aligned.apk
if [ ! -f phoneboard.keystore ]; then
  keytool -genkeypair -keystore phoneboard.keystore -storepass phoneboard -keypass phoneboard \
    -alias phoneboard -keyalg RSA -keysize 2048 -validity 10000 -dname "CN=PhoneBoard" >/dev/null 2>&1
fi
apksigner sign --ks phoneboard.keystore --ks-pass pass:phoneboard --key-pass pass:phoneboard \
  --min-sdk-version 21 --v4-signing-enabled false --out "$OUT" build/aligned.apk
apksigner verify "$OUT" && echo "APK: $OUT"
