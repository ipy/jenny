#!/bin/bash
set -e

# Usage: ./scripts/build.sh <version> <platform_set>
# platform_set: "all" or "native"
# Requires: go, upx (optional, skips compression if unavailable)

VERSION=$1
PLATFORM_SET=$2

if [ -z "$VERSION" ]; then
  echo "Error: VERSION is required as the first argument."
  exit 1
fi

LDFLAGS="-s -w -X github.com/ipy/jenny/internal/constants.Version=${VERSION}"

if [ "$PLATFORM_SET" = "all" ]; then
  platforms=("linux/amd64" "linux/arm64" "darwin/amd64" "darwin/arm64" "windows/amd64")
else
  platforms=("linux/amd64")
fi

for platform in "${platforms[@]}"; do
  platform_split=(${platform//\// })
  GOOS=${platform_split[0]}
  GOARCH=${platform_split[1]}
  output_name="jenny-$GOOS-$GOARCH"
  if [ "$GOOS" = "windows" ]; then
    output_name+='.exe'
  fi
  echo "Building $output_name (version=$VERSION)..."
  CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="$LDFLAGS" -trimpath -o "$output_name" ./cmd/jenny

  if [ "$GOOS" != "windows" ]; then
    chmod +x "$output_name"
  fi
done

# UPX compression (skipped if upx not available or on macOS for release binaries)
if command -v upx &>/dev/null; then
  echo ""
  echo "Applying UPX compression..."
  for binary in jenny-linux-amd64 jenny-linux-arm64; do
    [ -f "$binary" ] || continue
    # Skip if already UPX packed (idempotent)
    if upx -t "$binary" &>/dev/null; then
      echo "  $binary: already packed ($(du -sh "$binary" | cut -f1))"
      continue
    fi
    upx -9 "$binary"
    chmod +x "$binary"
    echo "  $binary: $(du -sh "$binary" | cut -f1)"
  done
  # macOS binaries: only compress locally if explicitly needed; skip for release
  # to avoid --force-macos causing issues on user machines
else
  echo ""
  echo "UPX not found — skipping binary compression."
  echo "Install UPX to reduce binary size: brew install upx (macOS) / apt install upx-ucl (Linux)"
fi

echo ""
echo "Build complete."

# Verify native binary builds correctly (version check)
HOST_GOOS=$(go env GOOS)
HOST_GOARCH=$(go env GOARCH)
NATIVE_BINARY="jenny-$HOST_GOOS-$HOST_GOARCH"
[ "$HOST_GOOS" = "windows" ] && NATIVE_BINARY+='.exe'

if [ -f "$NATIVE_BINARY" ]; then
  ./$NATIVE_BINARY --version
fi
