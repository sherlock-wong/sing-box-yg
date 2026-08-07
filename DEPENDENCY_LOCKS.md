# VPS dependency locks

[`assets/dependencies.json`](assets/dependencies.json) is the sole machine-readable source of truth for VPS core downloads. It is embedded into `vpnm` at build time and defines the supported architecture, HTTPS URL, SHA-256, archive type, and exact executable member for each core.

The current Go implementation locks only the dependencies retained by the new design:

- Sing-box 1.13.14 for amd64 and arm64
- Xray-core 26.3.27 for amd64 and arm64
- Realm 2.9.4 for amd64 and arm64
- Komari 1.4.2 for amd64 and arm64
- ACME adapter script commit `2feb392bd0e3964d9bf68871ae804578d9d5ca80`

Downloads are staged in a temporary directory and verified with SHA-256 before they are atomically installed. Archives are extracted with Go's standard ZIP/TAR readers; the Komari release is a checksum-verified raw executable. The manager rejects HTTP URLs, redirects, unsafe archive members, symlinks, and mismatched checksums.

Removed legacy components (WARP, Cloudflared, rule databases, `sbwpph`, and Sing-box 1.10.7) are intentionally not represented here.
