package processing

import (
	"bytes"
	"encoding/binary"
	"image"
	"io"
	"strings"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
	"github.com/gen2brain/webp"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
)

// Formats 同时供上传校验和接口使用，扩展名别名在这里统一声明。
func Formats() []string {
	return []string{"jpg", "jpeg", "png", "apng", "gif", "webp", "bmp", "tif", "tiff", "ico", "avif", "heic", "heif", "jxl", "svg"}
}

func Supported(format string) bool {
	for _, value := range Formats() {
		if value == format {
			return true
		}
	}
	return false
}

func ContentType(format string) string {
	switch format {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "svg":
		return "image/svg+xml"
	case "ico":
		return "image/vnd.microsoft.icon"
	case "tif", "tiff":
		return "image/tiff"
	default:
		return "image/" + format
	}
}

func BrowserImage(format string) bool {
	switch format {
	case "jpg", "jpeg", "png", "gif", "webp", "avif", "ico", "svg":
		return true
	}
	return false
}

// containerFormat 检查 ftyp 中的兼容品牌，避免把 AVIF 的通用 mif1 品牌误判为 HEIC。
func containerFormat(header []byte) string {
	if len(header) < 16 || string(header[4:8]) != "ftyp" {
		return ""
	}
	size := int(binary.BigEndian.Uint32(header[:4]))
	if size < 16 || size > len(header) {
		return ""
	}
	result := ""
	for offset := 8; offset+4 <= size; offset += 4 {
		if offset == 12 {
			continue
		}
		switch string(header[offset : offset+4]) {
		case "avif", "avis":
			return "avif"
		case "heic", "heix", "heim", "heis", "hevc", "hevx", "mif1", "msf1":
			result = "heic"
		}
	}
	return result
}

func decodeConfig(reader io.Reader, format string) (image.Config, error) {
	switch format {
	case "avif":
		return avif.DecodeConfig(reader)
	case "heic":
		return heic.DecodeConfig(reader)
	case "jxl":
		return jxl.DecodeConfig(reader)
	case "webp":
		return webp.DecodeConfig(reader)
	case "ico":
		frames, err := readIcons(reader, false)
		if err != nil {
			return image.Config{}, err
		}
		return frames[0].config, nil
	case "svg":
		_, cfg, err := readSVG(reader)
		return cfg, err
	}
	cfg, _, err := image.DecodeConfig(reader)
	return cfg, err
}

// Multiple 识别动画、多尺寸图标和多页 TIFF，供压缩逻辑决定处理方式。
func Multiple(data []byte, format string) bool {
	switch format {
	case "gif":
		return true
	case "webp":
		return len(data) >= 21 && string(data[12:16]) == "VP8X" && data[20]&2 != 0
	case "png":
		return bytes.Contains(data, []byte("acTL"))
	case "ico":
		return len(data) >= 6 && binary.LittleEndian.Uint16(data[4:6]) > 1
	case "tiff":
		if len(data) < 8 {
			return true
		}
		var order binary.ByteOrder = binary.LittleEndian
		if string(data[:2]) == "MM" {
			order = binary.BigEndian
		}
		offset := uint64(order.Uint32(data[4:8]))
		if offset+2 > uint64(len(data)) {
			return true
		}
		next := offset + 2 + uint64(order.Uint16(data[offset:]))*12
		return next+4 > uint64(len(data)) || order.Uint32(data[next:]) != 0
	case "avif", "heic":
		if len(data) < 16 {
			return true
		}
		size := min(uint64(binary.BigEndian.Uint32(data[:4])), uint64(len(data)))
		for offset := uint64(8); offset+4 <= size; offset += 4 {
			brand := string(data[offset : offset+4])
			if brand == "avis" || brand == "msf1" || strings.HasPrefix(brand, "hev") {
				return true
			}
		}
	}
	return false
}
