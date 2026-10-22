#!/bin/bash
# 将已构建的桌面二进制封装为带应用图标的 macOS bundle。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="${1:?用法：bash desktop/package-macos.sh <二进制路径> <输出.app路径> [版本号]}"
APP_BUNDLE="${2:?缺少输出.app路径}"
VERSION="${3:-0.0.0}"

if [[ ! -f "$BINARY" ]]; then
  echo "桌面二进制不存在：$BINARY" >&2
  exit 1
fi

mkdir -p "${APP_BUNDLE}/Contents/MacOS" "${APP_BUNDLE}/Contents/Resources"
cp "$BINARY" "${APP_BUNDLE}/Contents/MacOS/sec-desktop"
chmod +x "${APP_BUNDLE}/Contents/MacOS/sec-desktop"
cp "${SCRIPT_DIR}/assets/sec.icns" "${APP_BUNDLE}/Contents/Resources/sec.icns"
cp "${SCRIPT_DIR}/Info.plist" "${APP_BUNDLE}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleVersion ${VERSION}" "${APP_BUNDLE}/Contents/Info.plist"
/usr/libexec/PlistBuddy -c "Set :CFBundleShortVersionString ${VERSION}" "${APP_BUNDLE}/Contents/Info.plist"

# Go/链接器的裸二进制签名不包含 bundle 资源；所有内容写入后重新签署整个应用。
# ad-hoc 签名保证包完整性，不等同于 Developer ID 签名或 Apple 公证。
/usr/bin/codesign --force --sign - "$APP_BUNDLE"
/usr/bin/codesign --verify --deep --strict --verbose=2 "$APP_BUNDLE"
