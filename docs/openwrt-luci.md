# OpenWrt LuCI Support

The LuCI package design has moved to the standalone `localclash-luci` repo:

```text
/Volumes/Data/Github/localclash-luci/docs/openwrt-luci.md
```

This repository keeps the localClash core: CLI, MCP, runtime lifecycle,
configuration rendering, component downloads, and router takeover behavior.
The LuCI repository owns the OpenWrt package, LuCI frontend, rpcd ACL, and
LuCI helper integration layer.

For the local Docker OpenWrt and UTM OpenWrt test targets used to validate
that split, see `docs/openwrt-test-environments.md`.
