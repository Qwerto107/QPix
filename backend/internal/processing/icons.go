package processing

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"

	ico "github.com/thatoddmailbox/go-ico"
	"golang.org/x/image/bmp"
)

type iconFrame struct {
	entry  []byte
	data   []byte
	config image.Config
	image  image.Image
}

func readIcons(reader io.Reader, decode bool) ([]iconFrame, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
	if err != nil {
		return nil, err
	}
	invalid := errors.New("ICO 目录或图片数据无效（最多 64 个尺寸、8 MiB）")
	if len(data) < 6 || len(data) > 8<<20 || !bytes.Equal(data[:4], []byte{0, 0, 1, 0}) {
		return nil, invalid
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count < 1 || count > 64 || 6+count*16 > len(data) {
		return nil, invalid
	}
	frames := make([]iconFrame, count)
	best := 0
	for i := range frames {
		entry := data[6+i*16 : 22+i*16]
		size, offset := uint64(binary.LittleEndian.Uint32(entry[8:12])), uint64(binary.LittleEndian.Uint32(entry[12:16]))
		if size == 0 || offset < uint64(6+count*16) || offset+size > uint64(len(data)) {
			return nil, invalid
		}
		width, height := int(entry[0]), int(entry[1])
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		payload := data[offset : offset+size]
		cfg := image.Config{ColorModel: color.NRGBAModel, Width: width, Height: height}
		if bytes.HasPrefix(payload, []byte("\x89PNG\r\n\x1a\n")) {
			actual, err := png.DecodeConfig(bytes.NewReader(payload))
			if err != nil || actual.Width != width || actual.Height != height {
				return nil, invalid
			}
		} else {
			if len(payload) < 40 {
				return nil, invalid
			}
			headerSize := uint64(binary.LittleEndian.Uint32(payload[:4]))
			if headerSize < 40 || headerSize > uint64(len(payload)) || int32(binary.LittleEndian.Uint32(payload[4:8])) != int32(width) || int32(binary.LittleEndian.Uint32(payload[8:12])) != int32(height*2) {
				return nil, invalid
			}
		}
		frame := iconFrame{entry: bytes.Clone(entry), data: payload, config: cfg}
		if decode {
			// 逐张构造有界的单尺寸 ICO，避免第三方解码器一次展开整个目录。
			single := append([]byte{0, 0, 1, 0, 1, 0}, entry...)
			binary.LittleEndian.PutUint32(single[18:22], 22)
			single = append(single, payload...)
			decoded, err := ico.Decode(bytes.NewReader(single))
			if err != nil {
				return nil, err
			}
			frame.image = decoded.Images[0]
		}
		frames[i] = frame
		if width*height > frames[best].config.Width*frames[best].config.Height {
			best = i
		}
	}
	frames[0], frames[best] = frames[best], frames[0]
	return frames, nil
}

func encodeIcons(writer io.Writer, data []byte, quality int) error {
	frames, err := readIcons(bytes.NewReader(data), true)
	if err != nil {
		return err
	}
	var output bytes.Buffer
	output.Write([]byte{0, 0, 1, 0, byte(len(frames)), 0})
	offset := uint32(6 + len(frames)*16)
	for i := range frames {
		var encoded bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if err := encoder.Encode(&encoded, quantizePNG(frames[i].image, quality)); err != nil {
			return err
		}
		frames[i].data = encoded.Bytes()
		binary.LittleEndian.PutUint16(frames[i].entry[4:6], 1)
		binary.LittleEndian.PutUint16(frames[i].entry[6:8], 32)
		binary.LittleEndian.PutUint32(frames[i].entry[8:12], uint32(encoded.Len()))
		binary.LittleEndian.PutUint32(frames[i].entry[12:16], offset)
		output.Write(frames[i].entry)
		offset += uint32(encoded.Len())
	}
	for _, frame := range frames {
		output.Write(frame.data)
	}
	_, err = writer.Write(output.Bytes())
	return err
}

func quantizeRaster(source image.Image, quality int) image.Image {
	quantized := quantizePNG(source, quality)
	// BMP/TIFF 的索引编码不保存调色板 alpha，透明图片必须保持 RGBA。
	if opaque, ok := quantized.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		return quantized
	}
	target := image.NewNRGBA(source.Bounds())
	draw.Draw(target, target.Bounds(), quantized, quantized.Bounds().Min, draw.Src)
	return target
}

func encodeBMP(writer io.Writer, source image.Image, quality int) error {
	quantized := quantizeRaster(source, quality)
	if opaque, ok := quantized.(interface{ Opaque() bool }); ok && opaque.Opaque() {
		if quality < 100 {
			levels := 2 + quality*4/100
			palette := make(color.Palette, 0, levels*levels*levels)
			for r := 0; r < levels; r++ {
				for g := 0; g < levels; g++ {
					for b := 0; b < levels; b++ {
						palette = append(palette, color.NRGBA{uint8(r * 255 / (levels - 1)), uint8(g * 255 / (levels - 1)), uint8(b * 255 / (levels - 1)), 255})
					}
				}
			}
			indexed := image.NewPaletted(source.Bounds(), palette)
			for y := source.Bounds().Min.Y; y < source.Bounds().Max.Y; y++ {
				for x := source.Bounds().Min.X; x < source.Bounds().Max.X; x++ {
					c := color.NRGBAModel.Convert(source.At(x, y)).(color.NRGBA)
					r, g, b := int(c.R)*(levels-1)/255, int(c.G)*(levels-1)/255, int(c.B)*(levels-1)/255
					indexed.SetColorIndex(x, y, uint8((r*levels+g)*levels+b))
				}
			}
			quantized = indexed
		}
		return bmp.Encode(writer, quantized)
	}
	// BITMAPV4HEADER 显式声明 alpha 掩码，避免旧 40 字节 BMP 头丢失透明度。
	bounds := source.Bounds()
	header := make([]byte, 122)
	copy(header, "BM")
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(header[offset:], value) }
	put(2, uint32(len(header)+bounds.Dx()*bounds.Dy()*4))
	put(10, 122)
	put(14, 108)
	put(18, uint32(bounds.Dx()))
	put(22, uint32(bounds.Dy()))
	binary.LittleEndian.PutUint16(header[26:], 1)
	binary.LittleEndian.PutUint16(header[28:], 32)
	put(30, 3)
	put(34, uint32(bounds.Dx()*bounds.Dy()*4))
	put(54, 0x00ff0000)
	put(58, 0x0000ff00)
	put(62, 0x000000ff)
	put(66, 0xff000000)
	put(70, 0x73524742)
	if _, err := writer.Write(header); err != nil {
		return err
	}
	row := make([]byte, bounds.Dx()*4)
	for y := bounds.Max.Y - 1; y >= bounds.Min.Y; y-- {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(quantized.At(x, y)).(color.NRGBA)
			i := (x - bounds.Min.X) * 4
			row[i], row[i+1], row[i+2], row[i+3] = c.B, c.G, c.R, c.A
		}
		if _, err := writer.Write(row); err != nil {
			return err
		}
	}
	return nil
}
