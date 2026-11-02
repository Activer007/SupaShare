# Login artwork

Source PNGs and exact generation prompts: `docs/images/login-art-2026-10-31/`.
Selected UI reference: `docs/images/login-concepts-2026-10-31/login-03.png`.

Exported with `build/prepare-login-art.py`, Pillow, centered aspect-preserving fit,
1280 × 1056, WebP quality 82, method 6. Original PNGs are unchanged.

| File | Bytes |
| --- | ---: |
| linked-pair-v1.webp | 100664 |
| linked-chain-v1.webp | 101216 |
| shared-folder-v1.webp | 104714 |
| document-flow-v1.webp | 101358 |

Add/remove entries in `html_login_art.tmpl`; the first entry is the fallback.
Full extension and cache-versioning instructions: `docs/login-artwork.rst`.

## Selected expansion (2026-10-31)

Nine user-selected originals and prompts: `docs/images/login-exploration-2026-10-31/`.
That expansion brought the catalog to 13 images; linked-pair remains the fallback.

| File | Bytes |
| --- | ---: |
| vault-dial-v1.webp | 120156 |
| planetary-gears-v1.webp | 119302 |
| vault-linkage-v1.webp | 133632 |
| dial-edge-macro-v1.webp | 102862 |
| cipher-rotor-macro-v1.webp | 98042 |
| locking-cam-macro-v1.webp | 148814 |
| encrypted-ribbon-v1.webp | 74216 |
| amber-lightfold-v1.webp | 129538 |
| photon-stream-v1.webp | 127250 |

## User-provided expansion (2026-11-02)

Eight original PNGs, original filenames and SHA-256 values:
`docs/images/login-expansion-2026-11-02/`.
These artworks were supplied in the project’s `temp/` directory; generation tools
and prompts were not provided. Original bytes are preserved in the archive.
The complete catalog now has 21 images, with linked-pair still the fallback.

Exports use the same 1280 × 1056 centered fit and WebP method 6.
Seven use quality 82. The detailed cube uses quality 80 to stay below 300 KiB;
all eight are below that target and the 400 KiB catalog limit.

| File | Bytes | Quality |
| --- | ---: | ---: |
| cube-node-detail-v1.webp | 302594 | 80 |
| data-enigma-v1.webp | 104306 | 82 |
| data-library-v1.webp | 135286 | 82 |
| data-magic-v1.webp | 126006 | 82 |
| matrix-exchange-detail-v1.webp | 275168 | 82 |
| matrix-multichannel-v1.webp | 192180 | 82 |
| memory-matrix-v1.webp | 170776 | 82 |
| optical-pulses-v1.webp | 130828 | 82 |
