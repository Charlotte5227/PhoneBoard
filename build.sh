#!/bin/sh
# PhoneBoard 一括ビルド（Linux）: dist/PhoneBoard.exe と dist/PhoneBoard.apk を作る
#   必要: Go 1.22+, binutils-mingw-w64-x86-64(任意: exeアイコン用),
#         android-sdk-platform-23 aapt apksigner zipalign dalvik-exchange, JDK
set -e
cd "$(dirname "$0")"
mkdir -p dist
( cd pc
  if command -v x86_64-w64-mingw32-windres >/dev/null; then
    x86_64-w64-mingw32-windres --preprocessor=cpp --preprocessor-arg=-P -O coff -i app.rc -o rsrc_windows_amd64.syso
  fi
  go test ./...
  GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w" -o ../dist/PhoneBoard.exe . )
echo "EXE: dist/PhoneBoard.exe"
./android/build.sh ../dist/PhoneBoard.apk
