package processing

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/h265/heic"
	"github.com/gen2brain/jxl"
	"github.com/gen2brain/webp"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

func iconFixture(t *testing.T, sizes ...int) []byte {
	t.Helper()
	var header, frames bytes.Buffer
	header.Write([]byte{0, 0, 1, 0, byte(len(sizes)), 0})
	offset := 6 + 16*len(sizes)
	for _, size := range sizes {
		var payload bytes.Buffer
		if err := png.Encode(&payload, fixture(size, size, true)); err != nil {
			t.Fatal(err)
		}
		entry := make([]byte, 16)
		entry[0], entry[1] = byte(size), byte(size)
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(payload.Len()))
		binary.LittleEndian.PutUint32(entry[12:], uint32(offset))
		header.Write(entry)
		frames.Write(payload.Bytes())
		offset += payload.Len()
	}
	return append(header.Bytes(), frames.Bytes()...)
}

func TestAdditionalFormats(t *testing.T) {
	source := fixture(32, 32, true)
	for _, test := range []struct {
		name   string
		encode func(io.Writer) error
	}{
		{"bmp", func(w io.Writer) error { return bmp.Encode(w, source) }},
		{"tiff", func(w io.Writer) error { return tiff.Encode(w, source, &tiff.Options{Compression: tiff.Uncompressed}) }},
		{"ico", func(w io.Writer) error { _, err := w.Write(iconFixture(t, 32)); return err }},
		{"avif", func(w io.Writer) error {
			return avif.Encode(w, source, avif.Options{Quality: 95, QualityAlpha: 100, Speed: 10})
		}},
		{"heic", func(w io.Writer) error { return heic.Encode(w, source, heic.EncodeOptions{Quality: 95}) }},
		{"jxl", func(w io.Writer) error {
			return jxl.Encode(w, source, jxl.EncodeOptions{Quality: 95, Effort: 1, Threads: 1})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var encoded bytes.Buffer
			if err := test.encode(&encoded); err != nil {
				t.Fatal(err)
			}
			cfg, format, err := Config(encoded.Bytes())
			if err != nil || format != test.name || cfg.Width != 32 || cfg.Height != 32 {
				t.Fatal("格式或尺寸识别错误", format, cfg, err)
			}
			decoded, err := Decode(encoded.Bytes(), format)
			if err != nil {
				t.Fatal(err)
			}
			for _, mode := range []string{"preserve", "webp"} {
				result := Optimize(encoded.Bytes(), decoded, format, mode, 20)
				if result.Status != "optimized" && result.Status != "not_smaller" {
					t.Fatal(mode, "未接入压缩编码", result.Status)
				}
				actual, actualFormat, err := Config(result.Data)
				if err != nil || actual.Width != 32 || actual.Height != 32 || (mode == "preserve" && actualFormat != format) {
					t.Fatal("压缩结果格式或尺寸错误", actualFormat, actual, err)
				}
				output, err := Decode(result.Data, result.Format)
				if err != nil {
					t.Fatal(err)
				}
				if test.name != "bmp" && color.NRGBAModel.Convert(output.At(0, 0)).(color.NRGBA).A > 32 {
					t.Fatal("压缩丢失透明度", test.name, mode)
				}
			}
		})
	}
	data := iconFixture(t, 16, 32)
	decoded, err := Decode(data, "ico")
	if err != nil {
		t.Fatal(err)
	}
	compressed := Optimize(data, decoded, "ico", "preserve", 20)
	frames, err := readIcons(bytes.NewReader(compressed.Data), true)
	if err != nil || len(frames) != 2 || frames[0].config.Width != 32 {
		t.Fatal("ICO 压缩丢失尺寸", err)
	}
	converted := Optimize(data, decoded, "ico", "webp", 20)
	if converted.Status != "optimized" && converted.Status != "not_smaller" {
		t.Fatal("多尺寸 ICO 未接入 WebP 转换", converted.Status)
	}
	var transparent bytes.Buffer
	if err := encodeBMP(&transparent, source, 20); err != nil {
		t.Fatal(err)
	}
	bitmap, err := bmp.Decode(bytes.NewReader(transparent.Bytes()))
	if err != nil || color.NRGBAModel.Convert(bitmap.At(0, 0)).(color.NRGBA).A != 0 {
		t.Fatal("BMP alpha 掩码错误", err)
	}
}

func TestSVGAndContainerGuards(t *testing.T) {
	valid := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 24"><rect width="16" height="24" fill="green"/></svg>`)
	cfg, format, err := Config(valid)
	if err != nil || format != "svg" || cfg.Width != 16 || cfg.Height != 24 {
		t.Fatal(cfg, format, err)
	}
	if _, err := Decode(valid, "svg"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Config(append([]byte{0xef, 0xbb, 0xbf}, valid...)); err != nil {
		t.Fatal("SVG BOM 识别错误", err)
	}
	for _, data := range []string{`<svg><script/></svg>`, `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///secret">]><svg/>`, `<svg onload="run()"/>`, `<svg><image href="file:///secret"/></svg>`, `<html/>`} {
		if _, _, err := Config([]byte(data)); err == nil {
			t.Fatal("接收了活动文档", data)
		}
	}
	for _, data := range [][]byte{{0, 0, 1, 0, 255, 255}, {0, 0, 1, 0, 1, 0}, bytes.Repeat([]byte{255}, 64)} {
		if _, err := readIcons(bytes.NewReader(data), true); err == nil {
			t.Fatal("接收了非法 ICO")
		}
	}
	if !jxlAnimated([]byte{255, 10}) {
		t.Fatal("损坏 JXL 头未被拦截")
	}
}

func TestAnimatedWebPStaysOriginal(t *testing.T) {
	var data bytes.Buffer
	animation := &webp.WEBP{
		Image: []image.Image{fixture(16, 16, true), fixture(16, 16, false)},
		Delay: []int{100, 100},
	}
	if err := webp.EncodeAll(&data, animation); err != nil {
		t.Fatal(err)
	}
	_, format, err := Config(data.Bytes())
	if err != nil || format != "webp" || !Multiple(data.Bytes(), format) {
		t.Fatal("动画 WebP 识别错误", format, err)
	}
	decoded, err := Decode(data.Bytes(), format)
	if err != nil {
		t.Fatal("动画首帧解码失败", err)
	}
	for _, mode := range []string{"preserve", "webp"} {
		result := Optimize(data.Bytes(), decoded, format, mode, 20)
		if result.Status != "metadata" || !bytes.Equal(result.Data, data.Bytes()) {
			t.Fatal("动画被重编码或丢帧", mode, result.Status)
		}
	}
}
