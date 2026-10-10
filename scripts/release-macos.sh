#!/bin/bash
# Release pipeline for Veil.app: Release archive -> Developer ID export ->
# zip -> notarize -> staple -> DMG. Requires ~/.veil/keys/AuthKey_X4CUX6F3L4.p8.
set -euo pipefail
cd "$(dirname "$0")/.."

PROJ=apps/fill-safari/Veil.xcodeproj
SCHEME=Veil
TEAM=VFWGNKKT4G
KEY="$HOME/.veil/keys/AuthKey_X4CUX6F3L4.p8"
KEY_ID=X4CUX6F3L4
ISSUER=3808e100-9e6b-4dc3-bf18-270dba39ed94

ARCHIVE=/tmp/Veil.xcarchive
EXPORT=/tmp/Veil-export
ZIP=/tmp/Veil.zip
DMG=/tmp/Veil.dmg

[ -f "$KEY" ] || { echo "missing $KEY — download the App Store Connect key first"; exit 1; }

echo "== archive =="
rm -rf "$ARCHIVE"
xcodebuild -project "$PROJ" -scheme "$SCHEME" -configuration Release \
  -archivePath "$ARCHIVE" archive \
  ENABLE_USER_SCRIPT_SANDBOXING=NO | tail -3

echo "== export (Developer ID) =="
rm -rf "$EXPORT"
cat > /tmp/exportopts.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>method</key><string>developer-id</string>
	<key>teamID</key><string>$TEAM</string>
	<key>signingStyle</key><string>automatic</string>
</dict>
</plist>
EOF
xcodebuild -exportArchive -archivePath "$ARCHIVE" -exportPath "$EXPORT" \
  -exportOptionsPlist /tmp/exportopts.plist | tail -2

APP="$EXPORT/Veil.app"
codesign --verify --deep --strict "$APP"
echo "signature: $(codesign -dvv "$APP" 2>&1 | grep 'Authority=Developer ID' | head -1)"

echo "== zip + notarize =="
rm -f "$ZIP" && ditto -c -k --keepParent "$APP" "$ZIP"
xcrun notarytool submit "$ZIP" --key "$KEY" --key-id "$KEY_ID" --issuer "$ISSUER" --wait | tail -4

echo "== staple + DMG =="
xcrun stapler staple "$APP"
rm -f "$DMG"
hdiutil create -volname Veil -srcfolder "$APP" -ov -format UDZO "$DMG"
echo "done: $DMG"
