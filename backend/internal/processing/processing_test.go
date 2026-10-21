package processing

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/gen2brain/webp"
	xwebp "golang.org/x/image/webp"
)

func fixture(width, height int, transparent bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := uint8(255)
			if transparent {
				a = uint8((x + y) % 256)
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: uint8((x * y) % 256), A: a})
		}
	}
	return img
}

func TestAlphaAndFallback(t *testing.T) {
	source := fixture(128, 96, true)
	source.SetNRGBA(0, 0, color.NRGBA{R: 200, G: 100, B: 80, A: 0})
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatal(err)
	}
	result := Optimize(buffer.Bytes(), source, "png", "webp", 82)
	if result.Status != "optimized" || result.Format != "webp" || len(result.Data) >= buffer.Len() {
		t.Fatalf("未得到更小的 WebP: %s %d/%d", result.Status, len(result.Data), buffer.Len())
	}
	decoded, err := xwebp.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 96; y++ {
		for x := 0; x < 128; x++ {
			want := source.NRGBAAt(x, y)
			got := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if got.A != want.A {
				t.Fatalf("透明像素改变 (%d,%d): %+v != %+v", x, y, got, want)
			}
		}
	}
	for _, format := range []string{"gif"} {
		unchanged := Optimize(buffer.Bytes(), source, format, "webp", 82)
		if unchanged.Status != "unsupported" || !bytes.Equal(unchanged.Data, buffer.Bytes()) {
			t.Fatalf("%s 不应重新编码", format)
		}
	}
	// 不能安全转换的元数据必须保持原始文件字节。
	for _, chunk := range []string{"iCCP", "gAMA", "acTL", "eXIf"} {
		var extra bytes.Buffer
		_ = binary.Write(&extra, binary.BigEndian, uint32(0))
		extra.WriteString(chunk)
		_ = binary.Write(&extra, binary.BigEndian, crc32.ChecksumIEEE([]byte(chunk)))
		data := append(bytes.Clone(buffer.Bytes()[:33]), extra.Bytes()...)
		data = append(data, buffer.Bytes()[33:]...)
		unchanged := Optimize(data, source, "png", "webp", 82)
		if unchanged.Status != "metadata" || !bytes.Equal(unchanged.Data, data) {
			t.Fatalf("%s 未保留原文件", chunk)
		}
	}
	var high bytes.Buffer
	if err = png.Encode(&high, image.NewNRGBA64(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if Optimize(high.Bytes(), source, "png", "webp", 82).Status != "metadata" {
		t.Fatal("16 位 PNG 不应降为 8 位")
	}
	limited := limitedBuffer{limit: 2}
	if _, err = limited.Write([]byte("abc")); err != errLarger {
		t.Fatal("未限制编码输出大小")
	}
	large := boundedImage{Image: source, bounds: image.Rect(0, 0, 2001, 2000)}
	if result := Optimize(buffer.Bytes(), large, "png", "webp", 82); result.Status != "pixel_limit" || !bytes.Equal(result.Data, buffer.Bytes()) {
		t.Fatal("大图没有保留原文件")
	}
	cfg, format, err := Config(result.Data)
	if err != nil || format != "webp" || cfg.Width != 128 || cfg.Height != 96 {
		t.Fatal("静态 WebP 配置解析错误", err)
	}
}

func TestPerUploadQualityAndFormat(t *testing.T) {
	source := fixture(256, 192, true)
	var original bytes.Buffer
	// 不预压缩输入，确保测试能观察不同画质的输出，而不被体积回退掩盖。
	encoder := png.Encoder{CompressionLevel: png.NoCompression}
	if err := encoder.Encode(&original, source); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"preserve", "webp"} {
		t.Run(mode, func(t *testing.T) {
			low := Optimize(original.Bytes(), source, "png", mode, 20)
			high := Optimize(original.Bytes(), source, "png", mode, 95)
			if low.Status != "optimized" || high.Status != "optimized" || bytes.Equal(low.Data, high.Data) {
				t.Fatalf("画质未生效: %s/%s", low.Status, high.Status)
			}
			wantFormat := "png"
			if mode == "webp" {
				wantFormat = "webp"
			}
			for _, result := range []Result{low, high} {
				if result.Format != wantFormat {
					t.Fatal("输出格式错误", result.Format)
				}
				decoded, err := Decode(result.Data, result.Format)
				if err != nil {
					t.Fatal(err)
				}
				if decoded.Bounds() != source.Bounds() {
					t.Fatal("图片尺寸改变")
				}
				for y := 0; y < 192; y++ {
					for x := 0; x < 256; x++ {
						_, _, _, alpha := decoded.At(x, y).RGBA()
						if uint8(alpha>>8) != source.NRGBAAt(x, y).A {
							t.Fatalf("透明度改变: %d,%d", x, y)
						}
					}
				}
			}
		})
	}
	lossless := quantizePNG(source, 100)
	for y := 0; y < 192; y++ {
		for x := 0; x < 256; x++ {
			if color.NRGBAModel.Convert(lossless.At(x, y)).(color.NRGBA) != source.NRGBAAt(x, y) {
				t.Fatal("PNG 画质 100 不应减少色阶")
			}
		}
	}
	// 少色半透明图进入索引 PNG 分支时，同样不能改变透明度。
	paletteSource := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			paletteSource.SetNRGBA(x, y, color.NRGBA{R: 113, G: 89, B: 201, A: uint8((x % 4) * 85)})
		}
	}
	quantized := quantizePNG(paletteSource, 20)
	if _, ok := quantized.(*image.Paletted); !ok {
		t.Fatal("少色图片应使用索引 PNG")
	}
	var paletteOutput bytes.Buffer
	if err := png.Encode(&paletteOutput, quantized); err != nil {
		t.Fatal(err)
	}
	paletteDecoded, err := png.Decode(bytes.NewReader(paletteOutput.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	for x := 0; x < 64; x++ {
		_, _, _, alpha := paletteDecoded.At(x, 0).RGBA()
		if uint8(alpha>>8) != paletteSource.NRGBAAt(x, 0).A {
			t.Fatal("索引 PNG 透明度改变")
		}
	}
	for _, mode := range []string{"none", "invalid"} {
		if result := Optimize(original.Bytes(), source, "png", mode, 82); !bytes.Equal(result.Data, original.Bytes()) {
			t.Fatal("原文件改变")
		}
	}
	for _, quality := range []int{0, 101} {
		if Optimize(original.Bytes(), source, "png", "preserve", quality).Status != "failed" {
			t.Fatal("应拒绝无效画质")
		}
	}
	var jpegInput bytes.Buffer
	_ = jpeg.Encode(&jpegInput, fixture(256, 192, false), &jpeg.Options{Quality: 100})
	jpegSource, _ := Decode(jpegInput.Bytes(), "jpeg")
	low := Optimize(jpegInput.Bytes(), jpegSource, "jpeg", "preserve", 20)
	high := Optimize(jpegInput.Bytes(), jpegSource, "jpeg", "preserve", 80)
	if low.Status != "optimized" || high.Status != "optimized" || low.Format != "jpeg" || bytes.Equal(low.Data, high.Data) {
		t.Fatal("JPEG 原格式画质未生效")
	}
	var webpInput bytes.Buffer
	if err := webp.Encode(&webpInput, source, webp.Options{Lossless: true, Exact: true}); err != nil {
		t.Fatal(err)
	}
	webpSource, err := Decode(webpInput.Bytes(), "webp")
	if err != nil {
		t.Fatal(err)
	}
	webpLow := Optimize(webpInput.Bytes(), webpSource, "webp", "preserve", 10)
	if webpLow.Status != "optimized" || webpLow.Format != "webp" {
		t.Fatal("已有静态 WebP 未压缩", webpLow.Status)
	}
	// 已经很小的文件必须逐字节回退，不能保存更大的编码结果。
	if result := Optimize([]byte{1}, jpegSource, "jpeg", "preserve", 20); result.Status != "metadata" {
		t.Fatal("损坏元数据应跳过")
	}
	var tiny bytes.Buffer
	_ = png.Encode(&tiny, image.NewNRGBA(image.Rect(0, 0, 1, 1)))
	result := Optimize(tiny.Bytes(), image.NewNRGBA(image.Rect(0, 0, 1, 1)), "png", "webp", 100)
	if result.Status != "not_smaller" || !bytes.Equal(result.Data, tiny.Bytes()) {
		t.Fatal("无收益时未保留原文件", result.Status)
	}
}

type boundedImage struct {
	image.Image
	bounds image.Rectangle
}

func (i boundedImage) Bounds() image.Rectangle { return i.bounds }

func TestJPEGOrientationAndICC(t *testing.T) {
	source := fixture(128, 96, false)
	var original bytes.Buffer
	if err := jpeg.Encode(&original, source, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	// APP1 + 小端 TIFF：Orientation=6（顺时针旋转 90 度）。
	exif := []byte{'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, 6, 0, 0, 0, 0, 0, 0, 0}
	addSegment := func(code byte, payload []byte) []byte {
		data := bytes.Clone(original.Bytes()[:2])
		data = append(data, 0xff, code, byte((len(payload)+2)>>8), byte(len(payload)+2))
		data = append(data, payload...)
		return append(data, original.Bytes()[2:]...)
	}
	data := addSegment(0xe1, exif)
	decoded, err := Decode(data, "jpeg")
	if err != nil || decoded.Bounds().Dx() != 96 || decoded.Bounds().Dy() != 128 {
		t.Fatal("EXIF 方向没有校正", err)
	}
	result := Optimize(data, decoded, "jpeg", "webp", 82)
	if result.Status != "optimized" {
		t.Fatalf("JPEG 压缩失败: %s", result.Status)
	}
	converted, err := xwebp.Decode(bytes.NewReader(result.Data))
	if err != nil || converted.Bounds() != decoded.Bounds() {
		t.Fatal("转换后尺寸错误", err)
	}
	icc := addSegment(0xe2, []byte("ICC_PROFILE\x00fixture"))
	if result = Optimize(icc, source, "jpeg", "webp", 82); result.Status != "metadata" || !bytes.Equal(result.Data, icc) {
		t.Fatal("ICC 配置被丢失")
	}
}

// 合成样本仅比较当前实现的体积与耗时，不代表真实照片的压缩率。
func BenchmarkOptimize(b *testing.B) {
	for _, format := range []string{"jpeg", "png"} {
		b.Run(format, func(b *testing.B) {
			source := fixture(800, 600, format == "png")
			var data bytes.Buffer
			if format == "jpeg" {
				_ = jpeg.Encode(&data, source, &jpeg.Options{Quality: 95})
			} else {
				_ = png.Encode(&data, source)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result := Optimize(data.Bytes(), source, format, "webp", 82)
				b.ReportMetric(float64(data.Len()), "input-B")
				b.ReportMetric(float64(len(result.Data)), "output-B")
			}
		})
	}
}
