# Corresponding source

The source for the exact Remnanode Lite release is available from the GitHub
Release tag associated with this bundle:

https://github.com/luxiaba/remnanode-lite/releases

The bundled Xray-core executable is an unmodified, renamed build from the
commit recorded in `release-manifest.json`. Its corresponding source is
available from the XTLS/Xray-core repository. The exact GeoIP, GeoSite, and ASN
source revisions and redistributed payloads are recorded in
`runtime-assets.lock.json`; human-readable source links and attributions are in
`THIRD_PARTY_NOTICES.md`.

The immutable source locations currently pinned by the bundle are:

| Component | Source |
| --- | --- |
| Xray-core | https://github.com/XTLS/Xray-core/tree/5ca6f4b7d4dc20a881d4330e498892697627ec0c |
| GeoIP | https://github.com/Loyalsoldier/geoip/tree/e0cb5fd94679b83cd612a27e01ade2998fc24cdb |
| GeoIP snapshot | https://github.com/Loyalsoldier/geoip/archive/e0cb5fd94679b83cd612a27e01ade2998fc24cdb.tar.gz (`sha256:ac356437bcdefa3433a4eb100055debf0feda019f3757bcc25d31a1cb671d542`) |
| GeoSite | https://github.com/Loyalsoldier/v2ray-rules-dat/tree/27c9bd1c8ebd2a1eb871476ef10b6c157db0b460 |
| GeoSite snapshot | https://github.com/Loyalsoldier/v2ray-rules-dat/archive/27c9bd1c8ebd2a1eb871476ef10b6c157db0b460.tar.gz (`sha256:5be068756fcdbd6c01f04347833c34d31b0afa879a92f1e38a327a3a2273fe6b`) |
| ASN source | https://github.com/ipverse/as-ip-blocks/tree/56d021c7536afb15317155e45b57e7b5c87a4700 |
| ASN snapshot | https://github.com/ipverse/as-ip-blocks/archive/56d021c7536afb15317155e45b57e7b5c87a4700.tar.gz (`sha256:fc8be15bfbef3134f603276a26364935dbd2543d099dbaafa978a33b674a58ec`) |

These links are provided in addition to, and do not limit, any source-code
rights granted by the licenses in this bundle. If an upstream link becomes
unavailable, open an issue at:

https://github.com/luxiaba/remnanode-lite/issues
