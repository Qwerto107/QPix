package processing

import (
	"bytes"
	"encoding/binary"
)

// 只读取 JXL 的尺寸和动画标志，在解码器展开帧之前拒绝动画输入。
// 支持裸码流和容器中的首个 jxlc/jxlp 数据块；无法识别的头部按非静态处理。
func jxlAnimated(data []byte) bool {
	if bytes.HasPrefix(data, []byte("\x00\x00\x00\x0cJXL \r\n\x87\n")) {
		found := false
		for offset := uint64(12); offset+8 <= uint64(len(data)); {
			size := uint64(binary.BigEndian.Uint32(data[offset:]))
			header := uint64(8)
			if size == 1 {
				if offset+16 > uint64(len(data)) {
					return true
				}
				size, header = binary.BigEndian.Uint64(data[offset+8:]), 16
			}
			if size == 0 {
				size = uint64(len(data)) - offset
			}
			if size < header || size > uint64(len(data))-offset {
				return true
			}
			kind := string(data[offset+4 : offset+8])
			if kind == "jxlc" || kind == "jxlp" {
				start := offset + header
				if kind == "jxlp" {
					if start+4 > offset+size || binary.BigEndian.Uint32(data[start:])&0x7fffffff != 0 {
						return true
					}
					start += 4
				}
				data, found = data[start:offset+size], true
				break
			}
			offset += size
		}
		if !found {
			return true
		}
	}
	if len(data) < 3 || data[0] != 0xff || data[1] != 0x0a {
		return true
	}
	position, valid := 16, true
	bits := func(count int) uint32 {
		if position+count > len(data)*8 {
			valid = false
			return 0
		}
		var value uint32
		for i := 0; i < count; i++ {
			value |= uint32((data[position/8]>>uint(position%8))&1) << uint(i)
			position++
		}
		return value
	}
	size := func() {
		small := bits(1) != 0
		read := func() {
			if small {
				bits(5)
			} else {
				widths := [4]int{9, 13, 18, 30}
				bits(widths[bits(2)])
			}
		}
		read()
		if bits(3) == 0 {
			read()
		}
	}
	size()
	if bits(1) == 1 {
		return !valid
	}
	if bits(1) == 0 {
		return !valid
	}
	bits(3)
	if bits(1) == 1 {
		size()
	}
	if bits(1) == 1 {
		div8 := bits(1) != 0
		read := func() {
			selector := bits(2)
			if div8 {
				widths := [4]int{0, 0, 5, 9}
				bits(widths[selector])
			} else {
				widths := [4]int{6, 8, 10, 12}
				bits(widths[selector])
			}
		}
		read()
		if bits(3) == 0 {
			read()
		}
	}
	animated := bits(1) != 0
	return !valid || animated
}
