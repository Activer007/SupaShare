# README image sources

These images support the English and Chinese SupaShare READMEs. The current
README gallery uses `current-ui/`; the cover remains the original brand artwork.

## Current screenshots — 2026-11-02

The gallery was captured from a **2.16.0 development checkout** based on master
`4aadf84bea90d902e4272dd1445a282a1fdb3059`, with the eight-image login catalog
expansion in this change. It is not a recapture of the published release binary.
Chromium, DPR 1, loopback-only native macOS server, SQLite and local storage were
used. All accounts, filenames and file contents are synthetic demonstration data.
No real user data, passwords or API keys appear in the captures. Screenshots are
unmodified browser captures, not generated or redrawn product mockups.

| Files under `current-ui/` | Captured surface | CSS viewport |
| --- | --- | --- |
| `login-zh.jpg`, `login-en.jpg` | Chinese and English login, showing two of the new local illustrations | 1440 × 1000 |
| `workspace-zh.jpg`, `workspace-en.jpg` | Personal logical quota, upload rules and three ordinary shares | 1586 × 1100, full page |
| `collections-zh.jpg`, `collections-en.jpg` | Intake status, two selected received files, batch conversion controls and an active delivery list | 1586 × 1100, full page |
| `delivery-zh.jpg`, `delivery-en.jpg` | Public list containing two ordinary shares and individual download actions | 1440 × 1000 |

The login illustrations were selected deterministically for capture; the actual
page continues to choose randomly from its 21-entry catalog. The new original
artwork supplied by the user is preserved with its source filenames and SHA-256
values in [login-expansion-2026-11-02](login-expansion-2026-11-02/README.md).
Runtime WebP files are prepared separately from these screenshots; see
[login artwork maintenance](../login-artwork.rst).

## Cover and historical screenshots

The older files remain as historical evidence and are no longer the README gallery.

| File | Source |
| --- | --- |
| `supashare-cover.png` | Original brand artwork generated with the built-in Codex image generation tool on 2026-10-29. It is an illustration, not an application screenshot. |
| `workspace-en.jpg` | Real English workspace, SupaShare 2.12.0, B6 delivery sources based on B5 `4fae48a` plus mainline `811376b`; captured 2026-10-30 at CSS 1586×992, DPR 1. |
| `workspace-zh.jpg` | The same workspace in Simplified Chinese. |
| `permissions-en.jpg` | Earlier 2.11.0 permission editor, commit `8c39931733be853dd68d11ad5b193088c4d928d5`, opened for a demonstration ordinary user; no permission changes were saved. Its caption retains the captured version. |

The `ui-upgrade/` directory contains real per-batch screenshots from the 2.11.0
development cycle; their scope and viewport evidence are recorded in
`../ui-upgrade-execution.md`. They are not relabelled as 2.12.0 captures.

The screenshots were captured through the browser tool from a loopback-only, isolated SQLite/config/data fixture built from the source above. Account names and file content are synthetic. Screenshots contain no real user data, passwords or API keys. They have not been restyled or redrawn by image generation.

## Refreshing the screenshots

1. Build the target source version and generate its frontend/WASM assets.
2. Run a loopback-only local instance with disposable configuration and data. Use harmless demo filenames and a demonstration account.
3. Capture English and Chinese login pages with empty fields after the illustration has loaded. Record the selected illustration and source checkout.
4. Upload a few real demo files, reload the workspace to clear temporary upload highlights, and wait for quota and file rows to load before capturing.
5. Create a collection, submit demo files through its public link, then convert selected submissions into ordinary shares. Capture the collection and a public delivery list made from those shares.
6. Save browser screenshots as JPEG, inspect all captures, and update this source record and both README captions together. Preserve historical screenshots under their original provenance.

The screenshots show one demonstration configuration, not a default-data installation or a hosted public demo. Preserve this distinction when updating the documentation.

## Cover prompt

The built-in image generation tool was used with the following prompt. The accepted output was copied into this repository without compositing or image edits.

```text
Use case: ads-marketing. Asset type: wide GitHub README cover for SupaShare, a self-hosted desktop file sharing and file collection application.
Create a refined, restrained editorial product cover, landscape 2.4:1 aspect ratio, 1920 x 800 if possible. Dark charcoal (#1f2329) background matching a dark desktop file manager, electric blue (#1677ff) accents, soft studio light and subtle fine surface texture.
Composition: left 48% is calm typographic negative space. Large exact white wordmark "SupaShare", on one line. Beneath it smaller exact text "Self-hosted file sharing". Right half contains one beautifully crafted sculptural blue folder with two clean pale document sheets and a small metallic link symbol, resting on a dark surface; slight three-quarter perspective, sophisticated soft shadow, quiet blue rim light. Keep the object modest, not toy-like. Clear separation, generous margins, excellent readability when displayed at 900 px wide.
The visual should communicate control over files and straightforward sharing. Do not invent an app interface or browser screen. Do not add feature cards, badges, security certifications, shield icons, version numbers, extra logos, gradients across the text, watermarks, or any other words. The illustration is brand artwork, while actual application screenshots will appear below it in the README.
```
