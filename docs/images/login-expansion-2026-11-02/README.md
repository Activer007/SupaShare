# 用户提供的登录页插图（2026-11-02）

本目录保存用户放入项目 `temp/` 目录的 8 张原始 PNG，文件内容未修改。
归档时仅移除了文件名的数字序号；下表保留原文件名和 SHA-256 以便核对。
用户没有提供生成工具、提示词或更早的来源信息，此处不作推断。
这些是装饰插图，不是应用界面截图。

运行时只使用通过仓库现有脚本导出的 WebP：居中保持比例裁切、1280 × 1056、
method 6；7 张使用 quality 82，细节较多的 `cube-node-detail` 使用 quality 80，
确保每张 WebP 都在 300 KiB 以内。原图不随页面加载。

```bash
python3 build/prepare-login-art.py docs/images/login-expansion-2026-11-02/*.png --output-dir internal/webserver/web/static/assets/brand/login --version v1
python3 build/prepare-login-art.py docs/images/login-expansion-2026-11-02/cube-node-detail.png --output-dir internal/webserver/web/static/assets/brand/login --version v1 --quality 80
```

| 原文件名 | 归档文件名 | 原图字节数 | SHA-256 |
| --- | --- | ---: | --- |
| `01-data-magic.png` | [data-magic.png](data-magic.png) | 2211167 | `8339965311de84141a4c77c0446778adfed447816692ec8337e72b845c211709` |
| `01-optical-pulses.png` | [optical-pulses.png](optical-pulses.png) | 2185796 | `669ed1e740597cd9acb50a4d0a99278102f57e4a6c448dca235bb631c506ae00` |
| `02-cube-node-detail.png` | [cube-node-detail.png](cube-node-detail.png) | 3011674 | `eb7a1318d02b42e9dd6ba25e558c0fe2256b04f22b4dd1742b9cd62a943807bc` |
| `02-data-enigma.png` | [data-enigma.png](data-enigma.png) | 2175790 | `a80430b7abd9f67991ec0a1342885b2949749afd967e9a1ecd7826b22eccca35` |
| `03-data-library.png` | [data-library.png](data-library.png) | 2270113 | `f962362f1f163b502d9f9c830ce835b204894a37e5a0913e51461ae7ae93f7a4` |
| `03-matrix-multichannel.png` | [matrix-multichannel.png](matrix-multichannel.png) | 2482247 | `3a099372c15b146420210191a432eb55b21620387b5988b75002d120f228a2a6` |
| `04-matrix-exchange-detail.png` | [matrix-exchange-detail.png](matrix-exchange-detail.png) | 2891813 | `b548b4838507f35f36e530496e68bd9c0c7587b0aa2960b993e69be9396f30c7` |
| `04-memory-matrix.png` | [memory-matrix.png](memory-matrix.png) | 2372158 | `2b41f12aa6d9b8d7121f89e95fff77732c64d372a454d721e7eb1da36c81f3df` |

登录页沿用原有随机选择、失败回退和窄屏延迟加载行为；本次只扩充图集。
完整维护说明见 [login-artwork.rst](../../login-artwork.rst)。
