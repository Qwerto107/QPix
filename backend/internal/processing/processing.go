package processing

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"sync"

	"github.com/disintegration/imaging"
	"github.com/gen2brain/webp"
	xwebp "golang.org/x/image/webp"
)

const MaxPixels = 4_000_000

var encodeMu sync.Mutex

// Config 显式选择静态 WebP 解码器，避免多个库注册同一格式导致行为受导入顺序影响。
func Config(data []byte) (image.Config, string, error) {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		cfg, err := xwebp.DecodeConfig(bytes.NewReader(data))
		return cfg, "webp", err
	}
	return image.DecodeConfig(bytes.NewReader(data))
}

// Decode 校正 JPEG 的 EXIF 方向；WebP 使用静态解码器，避免静默丢失动画。
func Decode(data []byte, format string) (image.Image, error) {
	if format == "webp" {
		return xwebp.Decode(bytes.NewReader(data))
	}
	return imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
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
	if format != "jpeg" && format != "png" && format != "webp" {
		return result
	}
	if protectedMetadata(data, format) {
		result.Status = "metadata"
		return result
	}
	if _, cmyk := source.(*image.CMYK); cmyk {
		result.Status = "metadata"
		return result
	}
	// 纯 Go 编码有额外内存开销：大图保留原文件，编码串行执行以限制峰值。
	if int64(source.Bounds().Dx())*int64(source.Bounds().Dy()) > MaxPixels {
		result.Status = "pixel_limit"
		return result
	}
	encodeMu.Lock()
	defer encodeMu.Unlock()
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
	case "webp":
		err = webp.Encode(&output, source, webp.Options{Quality: quality, Method: 4, Exact: true})
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
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
			pixel.R, pixel.G, pixel.B = lookup[pixel.R], lookup[pixel.G], lookup[pixel.B]
			output.SetNRGBA(x, y, pixel)
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
			indexed.SetColorIndex(x, y, index)
		}
	}
	if indices != nil {
		indexed.Palette = palette
		return indexed
	}
	return output
}

func protectedMetadata(data []byte, format string) bool {
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
