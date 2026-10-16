package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"runtime"
)

// makeIcon 画一个带状态色的圆形锁图标；Windows 需要 ICO（内嵌 PNG）。
func makeIcon(c color.RGBA) []byte {
	const n = 32
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			dx, dy := float64(x)-15.5, float64(y)-15.5
			if dx*dx+dy*dy <= 15*15 {
				img.Set(x, y, c)
			}
			// 白色锁体与锁梁
			if x >= 10 && x <= 21 && y >= 15 && y <= 24 {
				img.Set(x, y, color.White)
			}
			r2 := dx*dx + (float64(y)-14)*(float64(y)-14)
			if y <= 15 && r2 <= 6*6 && r2 >= 4*4 {
				img.Set(x, y, color.White)
			}
		}
	}
	var p bytes.Buffer
	png.Encode(&p, img)
	if runtime.GOOS != "windows" {
		return p.Bytes()
	}
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, []uint16{0, 1, 1})
	b.Write([]byte{n, n, 0, 0})
	binary.Write(&b, binary.LittleEndian, []uint16{1, 32})
	binary.Write(&b, binary.LittleEndian, []uint32{uint32(p.Len()), 22})
	b.Write(p.Bytes())
	return b.Bytes()
}

var (
	iconGreen  = makeIcon(color.RGBA{22, 163, 74, 255})
	iconYellow = makeIcon(color.RGBA{202, 138, 4, 255})
	iconGray   = makeIcon(color.RGBA{107, 114, 128, 255})
	iconRed    = makeIcon(color.RGBA{220, 38, 38, 255})
)
