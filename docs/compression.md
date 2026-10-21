# 上传压缩

## 模式与画质

| 模式 | 行为 | 限制 |
| --- | --- | --- |
| none：保留原文件 | 文件字节不变 | 不应用画质 |
| preserve：原格式压缩 | JPEG、静态 WebP 按画质编码；PNG 按画质量化 RGB 色阶后用 DEFLATE 压缩 | 有损处理可能改变细节；PNG 透明通道逐像素保留 |
| webp：转换 WebP | JPEG、PNG、静态 WebP 使用有损 WebP，保留透明度 | 实际格式改为 WebP，显示名称保留上传名称 |

画质为 1–100 的整数，越高画质越好，不是目标体积或压缩百分比。不同格式的画质刻度不能直接等同。新安装默认 none、82；全局只设置默认值，显式上传参数优先。上传区及默认设置均使用实时显示数值的滑块，每张图片加入队列时固定方式与画质，重试沿用。旧开发版本的 smart 默认模式解释为 webp。

## PNG 算法取舍

本版采用纯 Go 的 RGB 均匀色阶量化，减少颜色精度；画质与每通道色阶数量使用二次映射（2–256 级），100 保留完整 8 位色阶。原始 alpha 不量化，包括半透明像素；量化后 RGBA 颜色总数不超过 256 时使用索引 PNG，否则使用 RGBA PNG。最后用标准库 PNG BestCompression 编码。无需外部命令、动态库或第三方图片上传。

[TinyPNG](https://tinypng.com/) 也通过颜色量化压缩 PNG，但本版不调用其服务，也不声称使用相同算法或达到相同体积。[pngquant / libimagequant](https://pngquant.org/lib/) 的自适应量化值得后续在画质样本上比较；当前选择优先保证纯 Go 部署与逐像素透明度不变。AVIF 暂不接入，避免增加编解码器与部署成本。

WebP 沿用固定版本 [gen2brain/webp v0.6.4](https://github.com/gen2brain/webp)，使用 `-tags=nodynamic`，不依赖 CGO 或系统共享库。编码 Quality 使用上传画质，Method=4，不再因 PNG 透明而切换到忽略画质的无损模式。格式与编码参数依据 [Google WebP 说明](https://developers.google.com/speed/webp/docs/compression) 和 [cwebp 参数](https://developers.google.com/speed/webp/docs/cwebp)。静态 WebP 解码使用 `golang.org/x/image/webp`。

## 回退与边界

- 仅采用严格小于原文件的结果；编码失败、无收益、超过限制均保存原文件并返回原因，不承诺固定压缩率。
- 成功后仅保存处理结果及缩略图，不额外备份原文件；实际格式以 format / URL 后缀为准。已有图片不受影响。
- GIF、APNG、非 8 位 PNG、带 ICC/gAMA/cHRM/PNG EXIF 的 PNG、JPEG ICC/CMYK 保留原文件；带 ICC/EXIF/XMP 的静态 WebP 同样跳过。动画 WebP 仍不在上传格式支持范围内。
- JPEG 转换前校正 EXIF 方向，压缩结果不携带原 EXIF/XMP；原文件模式不改字节，缩略图单独校正方向。
- 最多压缩 400 万像素，超过时保留原文件；上传原有 2500 万像素上限不变。编码串行执行，上传最多两个并发。
- 自动测试验证 PNG / WebP 不同画质输出不同、逐像素 alpha 不变、PNG 画质 100 完整色阶、JPEG 原格式、静态 WebP 再压缩、元数据与体积回退，以及上传参数对默认值的覆盖。

合成样本不代表真实照片画质或生产吞吐；实际收益取决于原图内容、已有压缩程度和选择的画质。
