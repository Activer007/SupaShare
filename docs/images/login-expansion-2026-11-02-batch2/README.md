# 用户提供的登录页链环插图（2026-11-02，第二批）

本目录保存用户放入项目 `temp/` 目录的 7 张原始 PNG，内容保持原始字节，
归档时改为描述画面主题的英文文件名。下表保留原文件名和 SHA-256 供核对。
用户未提供生成提示词或其他来源信息，不推断生成工具或更早的来源。
这些图片用于登录页右侧的装饰展示，不是应用界面截图。

7 张原图尺寸均为 1448 × 1086。运行时使用仓库现有脚本导出的 WebP：
居中保持比例裁切至 1280 × 1056、quality 82、method 6。
每张 WebP 均低于 300 KiB，原 PNG 不随页面加载。

```bash
python3 build/prepare-login-art.py docs/images/login-expansion-2026-11-02-batch2/*.png --output-dir internal/webserver/web/static/assets/brand/login --version v1
```

| 原文件名 | 归档文件名 | 原图字节数 | SHA-256 |
| --- | --- | ---: | --- |
| `ChatGPT 图像 2026年11月2日 17_02_00.png` | [stepped-link-pair.png](stepped-link-pair.png) | 2258835 | `35697581476f89e26bea1ae6d6d6a0b7396e2b62c6302a78eded5ff102e5e7c7` |
| `ChatGPT 图像 2026年11月2日 17_02_10.png` | [vertical-link-pair.png](vertical-link-pair.png) | 1769080 | `803bb7d2dcd344a88e8176af407c838295df0c143216b99d481108936386e931` |
| `ChatGPT 图像 2026年11月2日 17_02_17.png` | [diamond-link-weave.png](diamond-link-weave.png) | 2207937 | `146db715b1d93175fb9068a76a1a894eeb7769217a50346c4446ef9149384fa1` |
| `ChatGPT 图像 2026年11月2日 17_02_29.png` | [stepped-link-chain.png](stepped-link-chain.png) | 2105791 | `f0846619e705b3125debe6cdff081ea569a907c1cce6033e5a3d945d41e48d09` |
| `ChatGPT 图像 2026年11月2日 17_02_39.png` | [infinity-link-loop.png](infinity-link-loop.png) | 2143093 | `86760a46df463131eb37fd1395ee882fac6bbaebe4f92844d47cdff52deae555` |
| `ChatGPT 图像 2026年11月2日 19_38_26.png` | [rising-link-chain.png](rising-link-chain.png) | 2082110 | `1dbc83b016f9c6b731670962790c5187d773cf60c4b27450ec8b673dc76cdd14` |
| `ChatGPT 图像 2026年11月2日 19_38_37.png` | [diamond-link-pair.png](diamond-link-pair.png) | 2246347 | `6e5b353c08c423cd39e17d4d349819f4ff0c137f1096d38d36be25dbf976e8e9` |

| 运行时文件 | 字节数 | Quality |
| --- | ---: | ---: |
| `stepped-link-pair-v1.webp` | 108208 | 82 |
| `vertical-link-pair-v1.webp` | 42986 | 82 |
| `diamond-link-weave-v1.webp` | 96960 | 82 |
| `stepped-link-chain-v1.webp` | 94998 | 82 |
| `infinity-link-loop-v1.webp` | 89866 | 82 |
| `rising-link-chain-v1.webp` | 92868 | 82 |
| `diamond-link-pair-v1.webp` | 98550 | 82 |

既有 21 张插图、首项回退图和随机选择逻辑保持原样；新增条目后共 28 张。
居中导出会略裁原图左右边缘，主体保持居中。较矮登录窗口继续采用页面
原有的 `object-fit: cover`，按实际窗口比例显示局部画面。
维护流程见 [login-artwork.rst](../../login-artwork.rst)。
