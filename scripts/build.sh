#!/bin/bash
set -e

# Usage: ./scripts/build.sh <version> <platform_set>
# platform_set: "all" or "native"

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
  GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="$LDFLAGS" -v -o "$output_name" ./cmd/jenny

  if [ "$GOOS" != "windows" ]; then
    chmod +x "$output_name"
  fi
done

echo "Build complete."
./jenny-linux-amd64 --version
