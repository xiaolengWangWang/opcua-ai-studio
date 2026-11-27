#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
version=${1:-0.8.0}
if [[ ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo '用法: bash build/linux.sh [版本] [amd64|arm64|loong64 ...]' >&2
    exit 2
fi
if (($#)); then
    shift
fi
if ((! $#)); then
    set -- amd64 arm64 loong64
fi
for arch in "$@"; do
    case $arch in
        amd64|arm64|loong64) ;;
        *) echo "不支持的架构: $arch" >&2; exit 2 ;;
    esac
done

out="$PWD/dist/linux"
mkdir -p -- "$out"
stage=$(mktemp -d "$out/.build.XXXXXXXX")
cleanup() {
    rm -f -- "$stage/opcua-ai-studio" "$stage/package.tar" "$stage/package.tar.gz"
    rmdir -- "$stage"
}
trap cleanup EXIT

for arch in "$@"; do
    name="OPCUAAIStudio-$version-linux-$arch"
    GOOS=linux GOARCH="$arch" CGO_ENABLED=0 GOAMD64=v1 GOARM64=v8.0 GOLOONG64= go build \
        -trimpath -ldflags "-s -w -X main.version=$version" \
        -o "$stage/opcua-ai-studio" ./cmd/opcua-web
    # Set the archive modes explicitly for build hosts with no Unix file modes.
    transform="s|^|$name/|"
    tar -cf "$stage/package.tar" --mode=0755 --transform="$transform" -C "$stage" opcua-ai-studio
    tar -rf "$stage/package.tar" --mode=0644 --transform="$transform" LICENSE
    gzip -n -c "$stage/package.tar" > "$stage/package.tar.gz"
    mv -f -- "$stage/package.tar.gz" "$out/$name.tar.gz"
    (cd -- "$out"; sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
    echo "$out/$name.tar.gz"
done
