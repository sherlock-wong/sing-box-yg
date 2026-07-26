# VPS dependency locks

This list is the source of truth for executable VPS downloads. A dependency refresh must update this file, the constants in `sb.sh`, and the version notes together. Files are downloaded into `mktemp` directories, SHA-256 checked, and only then executed or installed.

| Name | Source | locked version / commit | download | SHA-256 |
| --- | --- | --- | --- | --- |
| Sing-box amd64 | SagerNet official release | 1.13.14 | `releases/download/v1.13.14/sing-box-1.13.14-linux-amd64.tar.gz` | `f48703461a15476951ac4967cdad339d986f4b8096b4eb3ff0829a500502d697` |
| Sing-box arm64 | SagerNet official release | 1.13.14 | `releases/download/v1.13.14/sing-box-1.13.14-linux-arm64.tar.gz` | `4742df6a4314e8ecc41736849fca6d73b8f9e91b6e8b06ee794ff17ba180579e` |
| Sing-box amd64 (legacy) | SagerNet official release | 1.10.7 | `releases/download/v1.10.7/sing-box-1.10.7-linux-amd64.tar.gz` | `1951a0785c8b4e1e21e0640227a49528ca772aec3d680061652e3d6b687e00fe` |
| Sing-box arm64 (legacy) | SagerNet official release | 1.10.7 | `releases/download/v1.10.7/sing-box-1.10.7-linux-arm64.tar.gz` | `15b43a0a50b4e6962aca819d4f3055aaac75ca7481350d4aaebe93ed06b7af49` |
| Xray-core amd64 | XTLS official release | 26.3.27 | `releases/download/v26.3.27/Xray-linux-64.zip` | `23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae` |
| Xray-core amd64 digest | XTLS official release | 26.3.27 | `releases/download/v26.3.27/Xray-linux-64.zip.dgst` | `052fc1c5c4bd5b44d799f785792a9631bce8da4aa0d385a783e9a711ad352a58` |
| Xray-core arm64 | XTLS official release | 26.3.27 | `releases/download/v26.3.27/Xray-linux-arm64-v8a.zip` | `4d30283ae614e3057f730f67cd088a42be6fdf91f8639d82cb69e48cde80413c` |
| Xray-core arm64 digest | XTLS official release | 26.3.27 | `releases/download/v26.3.27/Xray-linux-arm64-v8a.zip.dgst` | `1cafbf4fa746688990a12a6d344b638f706e531f1b81b8b583f9b2164561ad2f` |
| acme-yg | yonggekkk/acme-yg | `8ffa90d950ec9562248b8712634b335e8684e01b` | `raw.githubusercontent.com/yonggekkk/acme-yg/<commit>/acme.sh` | `5e43b5eea48987574730cecf77b5de483d4d7ec6e1e5f242b80f1321863f0521` |
| warp-yg | yonggekkk/warp-yg | `f2f634ba79452a0ffadcd93a6e6524cf4b7b84df` | `raw.githubusercontent.com/yonggekkk/warp-yg/<commit>/CFwarp.sh` | `7ebb2eba5c230d22643cdc77fdea0163877abcb0b5dde22b6b227f47523926d9` |
| BBR | teddysun/across | `fdb40962837b2e24bc94b87c2b1786ad2308489a` | `raw.githubusercontent.com/teddysun/across/<commit>/bbr.sh` | `17f447d78ba82468727e97cfdaa2a18150840a4c00c207592e5329df36544e85` |
| Cloudflared amd64 | Cloudflare official release | 2026.7.3 | `releases/download/2026.7.3/cloudflared-linux-amd64` | `9d71c677db00134c1bd4144b7783486b654ad281b1ea62b4972098d19f770f17` |
| Cloudflared arm64 | Cloudflare official release | 2026.7.3 | `releases/download/2026.7.3/cloudflared-linux-arm64` | `65259e652a7bea08bf5df603233ab22b8bf3116af8df9f9206209af6a1b955c0` |
| MetaCubeX geoip.db | MetaCubeX official `latest` release | resolved at install | `api.github.com/repos/MetaCubeX/meta-rules-dat/releases/latest` → asset API | required official API `digest` (`sha256`) |
| MetaCubeX geosite.db | MetaCubeX official `latest` release | resolved at install | `api.github.com/repos/MetaCubeX/meta-rules-dat/releases/latest` → asset API | required official API `digest` (`sha256`) |
| sbwpph amd64 | sherlock-wong/vps-net-manager | repository binary | `raw.githubusercontent.com/sherlock-wong/vps-net-manager/<branch>/sbwpph_amd64` | `93c7c5d7cb2c82cef44de782ae030b5f8fdb15038e3e95662e451bce7d3ee531` |
| sbwpph arm64 | sherlock-wong/vps-net-manager | repository binary | `raw.githubusercontent.com/sherlock-wong/vps-net-manager/<branch>/sbwpph_arm64` | `4a8f0419e4b848b99017128d532bd760f6daa4a7b0bc9f59ff166105db5c6e33` |
| Reality target candidates | sherlock-wong/vps-net-manager | repository text asset | `raw.githubusercontent.com/sherlock-wong/vps-net-manager/<branch>/assets/reality-targets.txt` | `eb83de80c1aaee01b11cceed5610ac3936ef7fbbcbfce49738a4a6503a010bda` |
| Realm amd64 | zhboner/realm official release | 2.9.4 | `releases/download/v2.9.4/realm-x86_64-unknown-linux-gnu.tar.gz` | `9dec109386b8abc828b452d0d1cecde35b7a2f8cfa93eae757fe9c248ad07ddd` |
| Realm arm64 | zhboner/realm official release | 2.9.4 | `releases/download/v2.9.4/realm-aarch64-unknown-linux-gnu.tar.gz` | `1f7f06e82fe0ea798b5c8e8e32906ee212a7085629a1c5cef9957ca270fcad99` |

For a user-requested Sing-box release other than the two defaults, the script fetches that release's official `sha256sum.txt`, finds the matching archive entry, and refuses installation when the checksum file or entry is unavailable. Xray installation verifies the pinned SHA-256 of the official `.dgst` file, parses its `SHA2-256` value, requires that value to equal the pinned archive hash, and then verifies the archive itself. MetaCubeX publishes its rule databases only under a replaceable `latest` release, so fixed asset IDs disappear on the next publish. The script therefore resolves the official release API on each install and refuses the download unless both required assets carry GitHub's official SHA-256 `digest`; each downloaded file is then checked against that digest. No unverified fallback is allowed.
