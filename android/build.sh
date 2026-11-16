#!/bin/sh
# PhoneBoard Android APK ビルド（Gradle不要。Ubuntu/Debian の android-sdk パッケージを使用）
#   sudo apt install android-sdk-platform-23 aapt apksigner zipalign dalvik-exchange default-jdk-headless
set -e
cd "$(dirname "$0")"
PLAT=${ANDROID_JAR:-/usr/lib/android-sdk/platforms/android-23/android.jar}
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
