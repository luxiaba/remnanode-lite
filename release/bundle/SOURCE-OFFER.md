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
| Xray-core | https://github.com/XTLS/Xray-core/tree/45cf2898ab12e97a55dd8f1f3d78d903340bdc9e |
| GeoIP | https://github.com/Loyalsoldier/geoip/tree/14643bbb36652911e928bfef799aa46372de1d7f |
| GeoIP snapshot | https://github.com/Loyalsoldier/geoip/archive/14643bbb36652911e928bfef799aa46372de1d7f.tar.gz (`sha256:e71b392b0d2c2d4f4203d93ef59f5d88f00765f84b7f2a46fdb0f6d9abb136d5`) |
| GeoSite | https://github.com/Loyalsoldier/v2ray-rules-dat/tree/31fa173fc342e550822ad25da6e0e28bed9e1f73 |
| GeoSite snapshot | https://github.com/Loyalsoldier/v2ray-rules-dat/archive/31fa173fc342e550822ad25da6e0e28bed9e1f73.tar.gz (`sha256:3ea2cf6fcd74ea3152c9419b44029c4995772f8f4f25e1e91a346d183a158cf6`) |
| ASN source | https://github.com/ipverse/as-ip-blocks/tree/56d021c7536afb15317155e45b57e7b5c87a4700 |
| ASN snapshot | https://github.com/ipverse/as-ip-blocks/archive/56d021c7536afb15317155e45b57e7b5c87a4700.tar.gz (`sha256:fc8be15bfbef3134f603276a26364935dbd2543d099dbaafa978a33b674a58ec`) |

These links are provided in addition to, and do not limit, any source-code
rights granted by the licenses in this bundle. If an upstream link becomes
unavailable, open an issue at:

https://github.com/luxiaba/remnanode-lite/issues
