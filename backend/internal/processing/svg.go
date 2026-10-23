package processing

import (
	"bytes"
	"encoding/xml"
	"errors"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func svgLength(value string) float64 {
	value = strings.TrimSpace(value)
	factor := 1.0
	for unit, scale := range map[string]float64{"px": 1, "pt": 96.0 / 72, "pc": 16, "mm": 96.0 / 25.4, "cm": 96.0 / 2.54, "in": 96} {
		if strings.HasSuffix(value, unit) {
			value = strings.TrimSuffix(value, unit)
			factor = scale
			break
		}
	}
	n, _ := strconv.ParseFloat(value, 64)
	return n * factor
}

func readSVG(reader io.Reader) ([]byte, image.Config, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	invalid := errors.New("SVG 无效、包含活动内容或超过 4 MiB")
	if err != nil {
		return nil, image.Config{}, err
	}
	if len(data) > 4<<20 {
		return nil, image.Config{}, invalid
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := xml.NewDecoder(bytes.NewReader(data))
	width, height := 0.0, 0.0
	depth, nodes, roots := 0, 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, image.Config{}, invalid
		}
		switch token := token.(type) {
		case xml.Directive:
			return nil, image.Config{}, invalid
		case xml.ProcInst:
			if token.Target != "xml" {
				return nil, image.Config{}, invalid
			}
		case xml.StartElement:
			depth++
			nodes++
			if depth > 64 || nodes > 20000 {
				return nil, image.Config{}, invalid
			}
			name := strings.ToLower(token.Name.Local)
			if name == "script" || name == "foreignobject" || name == "iframe" || name == "object" || name == "embed" {
				return nil, image.Config{}, invalid
			}
			for _, attr := range token.Attr {
				key, value := strings.ToLower(attr.Name.Local), strings.TrimSpace(attr.Value)
				if strings.HasPrefix(key, "on") || (key == "href" && !strings.HasPrefix(value, "#")) {
					return nil, image.Config{}, invalid
				}
			}
			if depth == 1 {
				roots++
				if roots != 1 || name != "svg" || (token.Name.Space != "" && token.Name.Space != "http://www.w3.org/2000/svg") {
					return nil, image.Config{}, invalid
				}
				viewWidth, viewHeight := 0.0, 0.0
				for _, attr := range token.Attr {
					switch attr.Name.Local {
					case "width":
						width = svgLength(attr.Value)
					case "height":
						height = svgLength(attr.Value)
					case "viewBox":
						values := strings.Fields(strings.ReplaceAll(attr.Value, ",", " "))
						if len(values) == 4 {
							viewWidth = svgLength(values[2])
							viewHeight = svgLength(values[3])
						}
					}
				}
				if width <= 0 {
					width = viewWidth
				}
				if height <= 0 {
					height = viewHeight
				}
				if width <= 0 {
					width = 300
				}
				if height <= 0 {
					height = 150
				}
			}
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(token)) != "" {
				return nil, image.Config{}, invalid
			}
		}
	}
	if roots != 1 || depth != 0 || math.IsNaN(width) || math.IsNaN(height) || math.IsInf(width, 0) || math.IsInf(height, 0) || width > 1_000_000 || height > 1_000_000 {
		return nil, image.Config{}, invalid
	}
	return data, image.Config{ColorModel: color.NRGBAModel, Width: int(math.Ceil(width)), Height: int(math.Ceil(height))}, nil
}

func decodeSVG(reader io.Reader) (image.Image, error) {
	data, cfg, err := readSVG(reader)
	if err != nil {
		return nil, err
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(data), oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	if icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return nil, errors.New("SVG 缺少可渲染视口")
	}
	// 静态矢量只生成缩略预览，原始 SVG 仍由浏览器以图片上下文显示。
	width, height := cfg.Width, cfg.Height
	if width > 1920 || height > 1920 {
		if width >= height {
			height = max(1, height*1920/width)
			width = 1920
		} else {
			width = max(1, width*1920/height)
			height = 1920
		}
	}
	target := image.NewNRGBA(image.Rect(0, 0, width, height))
	icon.SetTarget(0, 0, float64(width), float64(height))
	icon.Draw(rasterx.NewDasher(width, height, rasterx.NewScannerGV(width, height, target, target.Bounds())), 1)
	return target, nil
}
