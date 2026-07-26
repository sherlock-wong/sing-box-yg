# VPS dependency locks

[`assets/dependencies.json`](assets/dependencies.json) is the sole machine-readable source of truth for VPS core downloads. It is embedded into `vpnm` at build time and defines the supported architecture, HTTPS URL, SHA-256, archive type, and exact executable member for each core.

The current Go implementation locks only the dependencies retained by the new design:

- Sing-box 1.13.14 for amd64 and arm64
- Xray-core 26.3.27 for amd64 and arm64
- Realm 2.9.4 for amd64 and arm64
- ACME adapter script commit `8ffa90d950ec9562248b8712634b335e8684e01b`

Downloads are staged in a temporary directory, verified with SHA-256, and extracted using Go's standard ZIP/TAR readers before binaries are atomically installed. The manager rejects HTTP URLs, redirects, unsafe archive members, symlinks, and mismatched checksums.

Removed legacy components (WARP, Cloudflared, rule databases, `sbwpph`, and Sing-box 1.10.7) are intentionally not represented here.
