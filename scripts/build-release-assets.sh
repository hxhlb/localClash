#!/usr/bin/env sh
set -eu

version="${1:-}"
if [ -z "$version" ]; then
	echo "usage: scripts/build-release-assets.sh <version-or-tag>" >&2
	exit 2
fi

repo="${RELEASE_REPO:-qoli/localClash}"
dist="${DIST_DIR:-dist}"
mkdir -p "$dist"

build_asset() {
	goarch="$1"
	output="$dist/localclash-linux-$goarch"
	GOOS=linux GOARCH="$goarch" CGO_ENABLED=0 go build \
		-trimpath \
		-ldflags "-s -w -X main.version=$version" \
		-o "$output" .
	chmod 755 "$output"
	(cd "$dist" && sha256sum "localclash-linux-$goarch" > "localclash-linux-$goarch.sha256")
}

build_asset amd64
build_asset arm64

created_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
amd64_size="$(wc -c < "$dist/localclash-linux-amd64" | tr -d ' ')"
arm64_size="$(wc -c < "$dist/localclash-linux-arm64" | tr -d ' ')"
amd64_sha="$(cut -d ' ' -f 1 "$dist/localclash-linux-amd64.sha256")"
arm64_sha="$(cut -d ' ' -f 1 "$dist/localclash-linux-arm64.sha256")"
base_url="https://github.com/$repo/releases/download/$version"

cat > "$dist/localclash-release-manifest.json" <<EOF
{
  "schema_version": 1,
  "name": "localclash",
  "version": "$version",
  "created_at": "$created_at",
  "assets": [
    {
      "os": "linux",
      "arch": "amd64",
      "filename": "localclash-linux-amd64",
      "url": "$base_url/localclash-linux-amd64",
      "sha256": "$amd64_sha",
      "size": $amd64_size,
      "install_path": "/usr/local/bin/localclash"
    },
    {
      "os": "linux",
      "arch": "arm64",
      "filename": "localclash-linux-arm64",
      "url": "$base_url/localclash-linux-arm64",
      "sha256": "$arm64_sha",
      "size": $arm64_size,
      "install_path": "/usr/local/bin/localclash"
    }
  ]
}
EOF
