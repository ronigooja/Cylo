#!/usr/bin/env bash
set -euo pipefail

# Rebuild the iOS/macOS WebRTC framework from the upstream source revision
# corresponding to the pinned M155 binary package. All generated source and
# build outputs live under the dedicated Mac folder.
build_root=${ZOE_APPLE_BUILD_ROOT:-/Users/zhuwei/terminal/zoe-apple-build/webrtc-source}
source_revision=3b7ef6826ea6cb4c89c3972b1f14fcb3ed0fa469
depot_tools_revision=c84f5f5081e89eda268785a04a9e8a9a1987724f

case "$build_root" in
  /Users/zhuwei/terminal/zoe-apple-build/*) ;;
  *) echo "ZOE_APPLE_BUILD_ROOT must stay beneath /Users/zhuwei/terminal/zoe-apple-build" >&2; exit 2 ;;
esac

mkdir -p "$build_root"
cd "$build_root"

if [[ ! -d depot_tools/.git ]]; then
  git clone https://chromium.googlesource.com/chromium/tools/depot_tools.git depot_tools
fi
git -C depot_tools fetch origin "$depot_tools_revision"
git -C depot_tools checkout --detach "$depot_tools_revision"
export PATH="$build_root/depot_tools:$PATH"
export DEPOT_TOOLS_UPDATE=0

if [[ ! -f .gclient ]]; then
  fetch --nohooks webrtc_ios
fi
git -C src fetch origin "$source_revision"
git -C src checkout --detach "$source_revision"
gclient sync --with_branch_heads --with_tags

cd src
common_args='is_debug=false rtc_libvpx_build_vp9=true is_component_build=false rtc_include_tests=false rtc_enable_objc_symbol_export=true enable_stripping=true enable_dsyms=false use_lld=true rtc_ios_use_opengl_rendering=true rtc_system_openh264=true rtc_use_h265=true'

build_ios() {
  local arch=$1 environment=$2
  local dir="$build_root/out/ios-$arch-$environment"
  gn gen "$dir" --args="$common_args target_cpu=\"$arch\" target_os=\"ios\" target_environment=\"$environment\" ios_deployment_target=\"15.0\" ios_enable_code_signing=false"
  ninja -C "$dir" framework_objc
}

build_macos() {
  local arch=$1
  local dir="$build_root/out/macos-$arch"
  gn gen "$dir" --args="$common_args target_cpu=\"$arch\" target_os=\"mac\" mac_deployment_target=\"12.0\""
  ninja -C "$dir" mac_framework_objc
}

build_ios arm64 device
build_ios arm64 simulator
build_ios x64 simulator
build_macos arm64
build_macos x64

cd "$build_root/out"
mkdir -p package/ios-device package/ios-simulator package/macos-universal
cp -R ios-arm64-device/WebRTC.framework package/ios-device/
cp -R ios-arm64-simulator/WebRTC.framework package/ios-simulator/
cp -R macos-arm64/WebRTC.framework package/macos-universal/

lipo -create \
  ios-arm64-simulator/WebRTC.framework/WebRTC \
  ios-x64-simulator/WebRTC.framework/WebRTC \
  -output package/ios-simulator/WebRTC.framework/WebRTC
lipo -create \
  macos-arm64/WebRTC.framework/WebRTC \
  macos-x64/WebRTC.framework/WebRTC \
  -output package/macos-universal/WebRTC.framework/Versions/A/WebRTC

xcodebuild -create-xcframework \
  -framework package/ios-device/WebRTC.framework \
  -framework package/ios-simulator/WebRTC.framework \
  -framework package/macos-universal/WebRTC.framework \
  -output WebRTC-source.xcframework

plutil -lint WebRTC-source.xcframework/Info.plist
device_binary=WebRTC-source.xcframework/ios-arm64/WebRTC.framework/WebRTC
simulator_binary=WebRTC-source.xcframework/ios-arm64_x86_64-simulator/WebRTC.framework/WebRTC
macos_binary=WebRTC-source.xcframework/macos-arm64_x86_64/WebRTC.framework/Versions/A/WebRTC
test -f "$device_binary" && lipo -verify_arch arm64 "$device_binary"
test -f "$simulator_binary" && lipo -verify_arch arm64 x86_64 "$simulator_binary"
test -f "$macos_binary" && lipo -verify_arch arm64 x86_64 "$macos_binary"

echo "Source-built XCFramework: $build_root/out/WebRTC-source.xcframework"
echo "WebRTC source: $source_revision"
echo "depot_tools source: $depot_tools_revision"
