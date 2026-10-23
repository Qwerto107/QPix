package processing

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"sync"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
	"github.com/gen2brain/webp"
	"golang.org/x/image/tiff"
)

const MaxPixels = 4_000_000

var webpEncodeMu sync.Mutex

// Config 显式选择多格式解码器，避免格式注册顺序影响动画和容器识别。
func Config(data []byte) (image.Config, string, error) {
	return ConfigReader(bytes.NewReader(data))
}

func ConfigReader(source io.Reader) (image.Config, string, error) {
	reader := bufio.NewReader(source)
	header, _ := reader.Peek(512)
	format := containerFormat(header)
	switch {
	case len(header) >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WEBP":
		format = "webp"
	case bytes.HasPrefix(header, []byte{0, 0, 1, 0}):
		format = "ico"
	case bytes.HasPrefix(header, []byte{0xff, 0x0a}) || (len(header) >= 12 && string(header[4:8]) == "JXL "):
		format = "jxl"
	case bytes.HasPrefix(bytes.TrimSpace(bytes.TrimPrefix(header, []byte{0xef, 0xbb, 0xbf})), []byte("<")):
		format = "svg"
	}
	if format != "" {
		cfg, err := decodeConfig(reader, format)
		return cfg, format, err
	}
	return image.DecodeConfig(reader)
}

// Decode 仅解码用于预览的画面；Optimize 独立判断动画和多页，防止重编码丢失内容。
func Decode(data []byte, format string) (image.Image, error) {
	return DecodeReader(bytes.NewReader(data), format)
}

func DecodeReader(source io.Reader, format string) (image.Image, error) {
	if format == "webp" {
		return webp.Decode(source)
	}
	if format == "avif" {
		return avif.Decode(source, avif.Options{AutoRotate: true})
	}
	if format == "heic" {
		return heic.Decode(source, heic.Options{Threads: 1, AutoRotate: true, FrameSizeLimit: 100_000_000})
	}
	if format == "jxl" {
		data, err := io.ReadAll(source)
		if err != nil {
			return nil, err
		}
		if jxlAnimated(data) {
			return nil, errors.New("暂不支持动画 JXL")
		}
		return jxl.Decode(bytes.NewReader(data), jxl.Options{Threads: 1, FrameSizeLimit: 100_000_000})
	}
	if format == "ico" {
		frames, err := readIcons(source, true)
		if err != nil {
			return nil, err
		}
		return frames[0].image, nil
	}
	if format == "svg" {
		return decodeSVG(source)
	}
	if format == "jpeg" {
		return imaging.Decode(source, imaging.AutoOrientation(true))
	}
	decoded, _, err := image.Decode(source)
	return decoded, err
}

type Result struct {
	Data   []byte
	Format string
	Status string
}

var errLarger = errors.New("编码结果不小于原文件")

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) >= b.limit-b.Len() {
		return 0, errLarger
	}
	return b.Buffer.Write(p)
}

// Optimize 仅采用更小的结果；编码失败或无法保真处理的元数据均回退原文件。
func Optimize(data []byte, source image.Image, format, mode string, quality int) Result {
	result := Result{Data: data, Format: format, Status: "unsupported"}
	if mode == "none" {
		result.Status = "original"
		return result
	}
	if (mode != "preserve" && mode != "webp") || quality < 1 || quality > 100 {
		result.Status = "failed"
		return result
	}
	if !Supported(format) || format == "gif" || format == "svg" {
		return result
	}
	// ICO 原格式重编码保留所有尺寸；显式转 WebP 时使用已解码的最大尺寸。
	if protectedMetadata(data, format) || (Multiple(data, format) && format != "ico") {
		result.Status = "metadata"
		return result
	}
	if _, cmyk := source.(*image.CMYK); cmyk {
		result.Status = "metadata"
		return result
	}
	switch source.(type) {
	case *image.NRGBA64, *image.RGBA64, *image.Gray16:
		result.Status = "metadata"
		return result
	}
	// 纯 Go 编码有额外内存开销，大图保留原文件。
	if int64(source.Bounds().Dx())*int64(source.Bounds().Dy()) > MaxPixels {
		result.Status = "pixel_limit"
		return result
	}
	output := limitedBuffer{limit: len(data)}
	target := format
	if mode == "webp" {
		target = "webp"
	}
	var err error
	switch target {
	case "jpeg":
		err = jpeg.Encode(&output, source, &jpeg.Options{Quality: quality})
	case "png":
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		err = encoder.Encode(&output, quantizePNG(source, quality))
	case "bmp":
		err = encodeBMP(&output, source, quality)
	case "tiff":
		err = tiff.Encode(&output, quantizeRaster(source, quality), &tiff.Options{Compression: tiff.Deflate})
	case "ico":
		err = encodeIcons(&output, data, quality)
	case "avif":
		err = avif.Encode(&output, source, avif.Options{Quality: quality, QualityAlpha: 100, Speed: 8})
	case "heic":
		err = heic.Encode(&output, source, heic.EncodeOptions{Quality: quality})
	case "jxl":
		err = jxl.Encode(&output, source, jxl.EncodeOptions{Quality: quality, Effort: 3, Threads: 1})
	case "webp":
		// WebP 的纯 Go 模块有额外堆内存，单独串行；JPEG、PNG 可在上传处理槽位内并行。
		webpEncodeMu.Lock()
		defer webpEncodeMu.Unlock()
		err = webp.Encode(&output, source, webp.Options{Quality: quality, Method: 4, Exact: true})
	default:
		return result
	}
	if err != nil {
		result.Status = "failed"
		if errors.Is(err, errLarger) {
			result.Status = "not_smaller"
		}
		return result
	}
	if output.Len() >= len(data) {
		result.Status = "not_smaller"
		return result
	}
	return Result{Data: output.Bytes(), Format: target, Status: "optimized"}
}

// quantizePNG 按画质减少 RGB 色阶，保留每个像素的原始 alpha；100 使用完整色阶。
// 颜色不超过 256 种时使用索引 PNG，否则保留 RGBA，避免量化透明度或引入背景色。
func quantizePNG(source image.Image, quality int) image.Image {
	bounds := source.Bounds()
	output := image.NewNRGBA(bounds)
	levels := 2 + quality*quality*254/10000
	var lookup [256]uint8
	for i := range lookup {
		lookup[i] = uint8(((i*(levels-1)+127)/255*255 + (levels-1)/2) / (levels - 1))
	}
	palette := color.Palette{}
	indices := make(map[color.NRGBA]uint8)
	indexed := image.NewPaletted(bounds, nil)
	// 常见的 8 位 PNG 直接读取像素，兼容子图的非零原点与步长；其他色彩模型保留通用路径。
	nrgba, _ := source.(*image.NRGBA)
	rgba, _ := source.(*image.RGBA)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		out := output.Pix[(y-bounds.Min.Y)*output.Stride:]
		paletteRow := indexed.Pix[(y-bounds.Min.Y)*indexed.Stride:]
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			var pixel color.NRGBA
			switch {
			case nrgba != nil:
				i := (y-bounds.Min.Y)*nrgba.Stride + (x-bounds.Min.X)*4
				p := nrgba.Pix[i : i+4]
				pixel = color.NRGBA{R: p[0], G: p[1], B: p[2], A: p[3]}
			case rgba != nil:
				i := (y-bounds.Min.Y)*rgba.Stride + (x-bounds.Min.X)*4
				p := rgba.Pix[i : i+4]
				pixel.A = p[3]
				if p[3] == 255 {
					pixel.R, pixel.G, pixel.B = p[0], p[1], p[2]
				} else if p[3] != 0 {
					// RGBA 通道预乘了 alpha，按标准库规则还原为 NRGBA，不能把透明像素压到背景色。
					a := uint32(p[3])
					pixel.R = uint8((uint32(p[0]) * 0xffff / a) >> 8)
					pixel.G = uint8((uint32(p[1]) * 0xffff / a) >> 8)
					pixel.B = uint8((uint32(p[2]) * 0xffff / a) >> 8)
				}
			default:
				pixel = color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			}
			pixel.R, pixel.G, pixel.B = lookup[pixel.R], lookup[pixel.G], lookup[pixel.B]
			i := (x - bounds.Min.X) * 4
			out[i], out[i+1], out[i+2], out[i+3] = pixel.R, pixel.G, pixel.B, pixel.A
			if indices == nil {
				continue
			}
			index, exists := indices[pixel]
			if !exists {
				if len(palette) == 256 {
					indices = nil
					continue
				}
				index = uint8(len(palette))
				indices[pixel] = index
				palette = append(palette, pixel)
			}
			paletteRow[x-bounds.Min.X] = index
		}
	}
	if indices != nil {
		indexed.Palette = palette
		return indexed
	}
	return output
}

func protectedMetadata(data []byte, format string) bool {
	if format != "jpeg" && format != "png" && format != "webp" {
		return false
	}
	if format == "webp" {
		// VP8X 的动画、ICC、EXIF、XMP 标志：不能静默丢弃动画或色彩信息。
		return len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&0x2e != 0
	}
	if format == "png" {
		// WebP 仅支持 8 位通道；APNG、色彩配置和 PNG EXIF 暂不转换。
		if len(data) < 25 || data[24] != 8 {
			return true
		}
		for offset := 8; offset+12 <= len(data); {
			size := uint64(binary.BigEndian.Uint32(data[offset:]))
			if size+12 > uint64(len(data)-offset) {
				return true
			}
			switch string(data[offset+4 : offset+8]) {
			case "acTL", "iCCP", "gAMA", "cHRM", "eXIf":
				return true
			}
			offset += int(size) + 12
		}
		return false
	}
	// JPEG ICC 配置位于 APP2；不丢弃配置后直接重新解释颜色。
	reader := bytes.NewReader(data)
	if _, err := reader.Seek(2, io.SeekStart); err != nil {
		return true
	}
	for reader.Len() > 0 {
		marker, err := reader.ReadByte()
		if err != nil || marker != 0xff {
			return true
		}
		code, err := reader.ReadByte()
		if err != nil {
			return true
		}
		for code == 0xff {
			code, err = reader.ReadByte()
			if err != nil {
				return true
			}
		}
		if code == 0xda || code == 0xd9 {
			return false
		}
		if code == 0x01 || code >= 0xd0 && code <= 0xd7 {
			continue
		}
		var size uint16
		if binary.Read(reader, binary.BigEndian, &size) != nil || size < 2 || int(size)-2 > reader.Len() {
			return true
		}
		payload := make([]byte, int(size)-2)
		if _, err := io.ReadFull(reader, payload); err != nil {
			return true
		}
		if code == 0xe2 && bytes.HasPrefix(payload, []byte("ICC_PROFILE\x00")) {
			return true
		}
		if (code == 0xc0 || code == 0xc1 || code == 0xc2) && len(payload) >= 6 && payload[5] == 4 {
			return true
		}
	}
	return true
}
