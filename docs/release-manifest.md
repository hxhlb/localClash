# Release Manifest

localClash publishes router-installable core binaries from GitHub Releases. The
LuCI helper consumes `localclash-release-manifest.json`, selects the current
router architecture, verifies `sha256`, and atomically installs the binary to:

```text
/usr/local/bin/localclash
```

Release assets:

```text
localclash-linux-amd64
localclash-linux-arm64
localclash-linux-amd64.sha256
localclash-linux-arm64.sha256
localclash-base-assets.tar.gz
localclash-base-assets.tar.gz.sha256
localclash-release-manifest.json
```

`localclash-base-assets.tar.gz` contains the disk assets the CLI expects to
find in its working directory:

```text
policies/
policy-templates/
rule-sources/
.runtime/mihomo/Country.mmdb
.runtime/mihomo/geoip.dat
.runtime/mihomo/geosite.dat
.runtime/mihomo/ASN.mmdb
```

Manifest shape:

```json
{
  "schema_version": 1,
  "name": "localclash",
  "version": "v0.1.0",
  "created_at": "2026-05-21T00:00:00Z",
  "assets": [
    {
      "os": "linux",
      "arch": "arm64",
      "filename": "localclash-linux-arm64",
      "url": "https://github.com/qoli/localClash/releases/download/v0.1.0/localclash-linux-arm64",
      "sha256": "...",
      "size": 12345678,
      "install_path": "/usr/local/bin/localclash"
    }
  ],
  "base_assets": {
    "filename": "localclash-base-assets.tar.gz",
    "url": "https://github.com/qoli/localClash/releases/download/v0.1.0/localclash-base-assets.tar.gz",
    "sha256": "...",
    "size": 123456,
    "install_path": "/root/localclash",
    "contents": [
      "policies/",
      "policy-templates/",
      "rule-sources/",
      ".runtime/mihomo/Country.mmdb",
      ".runtime/mihomo/geoip.dat",
      ".runtime/mihomo/geosite.dat",
      ".runtime/mihomo/ASN.mmdb"
    ]
  }
}
```

The release workflow is `.github/workflows/release.yml`. It runs the Go test
suite first, then builds linux `amd64` and `arm64` binaries with:

```bash
scripts/build-release-assets.sh v0.1.0
```

The workflow runs on tag pushes matching `v*` and can also be triggered
manually with a tag input.
