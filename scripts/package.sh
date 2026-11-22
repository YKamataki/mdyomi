#!/usr/bin/env bash
# Build the six release archives and smoke-test the Linux/amd64 package.
set -euo pipefail

version=${1:?usage: bash scripts/package.sh VERSION NEW_OUTPUT_DIRECTORY}
output=${2:?missing output directory}
if [[ ! "$version" =~ ^(v[0-9]+\.[0-9]+\.[0-9]+|snapshot-[0-9a-f]+)$ ]]; then
  echo "Invalid distribution version: $version" >&2
  exit 1
fi

mkdir -- "$output"
output=$(realpath "$output")
cd "$(dirname "$0")/.."
stage=$(mktemp -d)
trap 'rm -rf -- "$stage"' EXIT

go_license="$(go env GOROOT)/LICENSE"
if [[ ! -f "$go_license" ]]; then go_license=/usr/share/licenses/go/LICENSE; fi
cp README.md LICENSE "$stage/"
cp -R LICENSES "$stage/"
cp "$go_license" "$stage/LICENSES/Go.txt"

for os in linux darwin windows; do
  for arch in amd64 arm64; do
    exe=mdyomi
    if [[ "$os" == windows ]]; then exe=mdyomi.exe; fi
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -mod=readonly -trimpath -ldflags='-s -w' -o "$stage/$exe" .
    archive="$output/mdyomi-${version}-${os}-${arch}"
    if [[ "$os" == windows ]]; then
      (cd "$stage" && zip -qr "$archive.zip" "$exe" README.md LICENSE LICENSES)
      unzip -tq "$archive.zip"
    else
      tar -C "$stage" -czf "$archive.tar.gz" "$exe" README.md LICENSE LICENSES
      tar -tzf "$archive.tar.gz" >/dev/null
    fi
  done
done

(cd "$output" && sha256sum -- *.tar.gz *.zip > SHA256SUMS && sha256sum -c SHA256SUMS)
tar -xzf "$output/mdyomi-${version}-linux-amd64.tar.gz" -C "$stage" mdyomi
printf 'OSとUSB\r\n' > "$stage/input"
env -u OPENAI_API_KEY "$stage/mdyomi" < "$stage/input" > "$stage/output"
cmp "$stage/input" "$stage/output"
