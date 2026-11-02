# SupaShare 正式 Logo：连接之 S（2026-11-02）

用户从本轮 Logo 选型中选择方案 1「连接之 S」作为项目默认标识。蓝色与银灰色的双链形成 S，横版包含 SupaShare 字标；独立图标用于默认 favicon。

这两张 PNG 由内置 `image_gen` 工具生成，归档文件保留生成输出的原始字节。横版最初尝试透明底，随后通过内置工具修复文字边缘并改为不透明深色底；本目录归档的是选定的修复后版本及其配套独立图标。图片不是 SVG，也不含真实透明背景。

| 生成选型文件名 | 本目录归档文件 | 尺寸 |
| --- | --- | --- |
| `01-connection-s.png` | [connection-s-original.png](connection-s-original.png) | 2172 × 724 |
| `04-connection-s-icon.png` | [connection-s-icon-original.png](connection-s-icon-original.png) | 1254 × 1254 |

只归档选定方案及其图标；本地对比方案和试装脚本不作为运行依赖。

## 运行资源的技术导出

### 横版 Logo

运行文件：`internal/webserver/web/static/assets/brand/supashare-logo-v1.webp`。

复用已验证的网页试装资源，字节与原 `page-preview/logo-1.webp` 完全相同。其处理参数为：横版原图 RGB，半开裁切框 `(54, 164, 2118, 584)`，Lanczos 保持比例缩放到 **1000 × 203**，WebP quality **94**、method **6**。仅裁去多余画布并缩放、压缩；没有重新绘制图形或文字。

### 默认 favicon

运行源文件：`internal/webserver/web/static/defaultFavicon.png`。

独立图标原图转为 RGB，以半开裁切框 **`(202, 191, 1042, 1031)`** 取得 840 × 840 正方形，再以 Lanczos 缩放到 **512 × 512**，保存为 PNG（`optimize=True, compress_level=9`）。保留不透明深色底，没有填充、重绘或合成。

裁切框围绕源图前景范围 `(270, 283, 975, 939)` 居中，图形宽度占裁切画布约 **84%**，以改善小图标的识别。前景范围仅用于确定裁切位置，按 RGB 三通道最大值大于 110 的像素估计；输出保留裁切框内的全部原像素内容。使用 Pillow **12.2.0**。

既有 favicon 模块继续从此 512 × 512 PNG 生成 ICO 和 16/32/180/192/512 像素端点；自定义 favicon 覆盖机制由应用保留。

## 字节数与 SHA-256

| 文件 | 字节数 | SHA-256 |
| --- | ---: | --- |
| `connection-s-original.png` | 1214523 | `a654daad4d1546469469d350d140e905711305da1a94b9f920d1c19b1aaa5d1c` |
| `connection-s-icon-original.png` | 1199712 | `abe94997ea3e1d5f264234a9f429d2a2f2b788baa166d8db23a2e33019cd2761` |
| `supashare-logo-v1.webp` | 16484 | `c6b2380ba6741c8cc0b1e86861c938e9e7993fd5ce2875cf4a7df10c2ecb7635` |
| `defaultFavicon.png` | 185566 | `4fac2d6e9d285793313332854bfcdeefdd3f5d174372dea98dace3aceabd26fd` |

## 完整生成提示词

### 方案 1：初稿

```text
Use case: logo-brand. Asset type: production-ready PNG logo concept for SupaShare, a self-hosted file collection and delivery application for individuals and small teams. Create ONE sophisticated horizontal logo, symbol plus exact wordmark "SupaShare" (capital S, lowercase upa, capital S, lowercase hare). Concept 01: CONNECTION S. A distinctive compact geometric S-shaped monogram constructed from two interlocking open link forms, suggesting a flowing file handoff and human connection, with strong balanced negative space. Simple bold flat vector-style geometry that stays recognizable at 24 pixels. Electric blue #1677FF and a restrained silver #AEBCCA on the symbol; near-white #F4F7FC wordmark designed for the project's charcoal dark interface. Confident modern humanist sans serif lettering, carefully kerned, medium-bold; excellent balance between icon and text. Wide approximately 3:1 canvas, generous clearspace all around; icon at left and wordmark to its right, vertically aligned. Genuinely transparent background with clean alpha edges. No background rectangle, checkerboard graphic, mockup, 3D, gradients, lighting, shadows, texture, tiny details, slogan, extra lettering, trademark symbols, or padlock. This is a standalone usable logo asset, not a presentation board. Prioritize a unique coherent silhouette and exceptionally clean professional typography.
```

### 方案 1：修复边缘并改为深色底

```text
Use case: logo-brand / precise-object-edit. Input image is the edit target: the SupaShare connection-S logo that was just generated. Preserve the exact S-shaped two-link symbol silhouette, blue upper link and silver lower link, the exact spelling "SupaShare", and the overall horizontal arrangement. Make this a pristine polished logo concept on a fully OPAQUE uniform charcoal #212529 background, NOT transparent. Repair ALL distressed, missing, speckled, eroded, scratched and perforated areas in the letters and the icon; every stroke must be completely solid with smooth crisp antialiased edges. Remove every stray fleck, dark internal scratch, rough outline, glitch and artifact. Use flat solid colors: #1677FF blue and #AEBCCA silver symbol, #F4F7FC solid wordmark. No gradient, no shadow, no outlines, no texture. Keep generous clearspace around the entire logo with at least 8% left and right padding, no cropping. Exact professional carefully kerned text only "SupaShare". Wide landscape composition. This should look like a finished clean vector identity rendered to PNG, not distressed lettering or a presentation mockup.
```

### 方案 1：独立图标

```text
Use case: logo-brand / precise-object-edit. The supplied horizontal SupaShare logo is the reference and edit target. Create its matching standalone square app icon. Preserve ONLY its existing compact S-shaped interlocking two-link mark: blue #1677FF upper link, cool silver #AEBCCA lower link, the identical geometry, proportions and negative-space openings. Completely remove the "SupaShare" lettering and do not add any text. Center this same mark on a square 1:1 fully opaque uniform charcoal #212529 canvas, with a generous safe margin of approximately 20 percent of the canvas on every side. Keep it optically centered and balanced. Smooth solid clean edges, no eroded pixels or specks. The intended use is a GitHub repository avatar, application icon, and favicon concept, so the silhouette must be bold and remain readable when reduced. Do not add a new icon design, rounded background tile, border, shadow, 3D extrusion, glow, texture, decoration, multiple versions or a product mockup. No transparency.
```
