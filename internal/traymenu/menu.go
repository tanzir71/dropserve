// Package traymenu defines Dropserve's optional system-tray presentation.
package traymenu

import "encoding/binary"

// State is a visible tray-icon state.
type State uint8

const (
	// Running is the normal healthy state.
	Running State = iota
	// Warning means an app, port, or local integration needs attention.
	Warning
	// Sharing means a public sharing path is active.
	Sharing
	// Paused means serving has been intentionally stopped.
	Paused
)

// Labels returns the actionable menu items in display order.
func Labels() []string {
	return []string{
		"Open Dashboard",
		"Open Apps Folder",
		"Copy LAN Link",
		"Pause Serving",
		"Start at Login",
		"Run Doctor",
		"Quit",
	}
}

// Icon creates a small self-contained Windows ICO for state. Other tray
// backends also accept the embedded 32-bit bitmap representation.
func Icon(state State) []byte {
	color := [3]byte{0x76, 0xc9, 0x59}
	switch state {
	case Warning:
		color = [3]byte{0x36, 0x9a, 0xf5}
	case Sharing:
		color = [3]byte{0xed, 0x73, 0x8a}
	case Paused:
		color = [3]byte{0x8d, 0x84, 0x78}
	case Running:
	}

	const (
		width       = 16
		height      = 16
		bitmapBytes = width * height * 4
		maskBytes   = 4 * height
		imageBytes  = 40 + bitmapBytes + maskBytes
		imageOffset = 22
	)
	icon := make([]byte, imageOffset+imageBytes)
	binary.LittleEndian.PutUint16(icon[2:4], 1)
	binary.LittleEndian.PutUint16(icon[4:6], 1)
	icon[6] = width
	icon[7] = height
	binary.LittleEndian.PutUint16(icon[10:12], 1)
	binary.LittleEndian.PutUint16(icon[12:14], 32)
	binary.LittleEndian.PutUint32(icon[14:18], imageBytes)
	binary.LittleEndian.PutUint32(icon[18:22], imageOffset)

	dib := icon[imageOffset:]
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], width)
	binary.LittleEndian.PutUint32(dib[8:12], height*2)
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 32)
	binary.LittleEndian.PutUint32(dib[20:24], bitmapBytes)

	pixels := dib[40 : 40+bitmapBytes]
	for displayY := 0; displayY < height; displayY++ {
		for x := 0; x < width; x++ {
			bottomUpY := height - 1 - displayY
			offset := (bottomUpY*width + x) * 4
			if transparentCorner(x, displayY) {
				continue
			}
			pixel := color
			if dropserveGlyph(x, displayY) {
				pixel = [3]byte{0xff, 0xff, 0xff}
			}
			pixels[offset] = pixel[0]
			pixels[offset+1] = pixel[1]
			pixels[offset+2] = pixel[2]
			pixels[offset+3] = 0xff
		}
	}
	return icon
}

func transparentCorner(x, y int) bool {
	return (x < 2 || x > 13) && (y < 2 || y > 13)
}

func dropserveGlyph(x, y int) bool {
	if x == 5 && y >= 4 && y <= 11 {
		return true
	}
	if (y == 4 || y == 11) && x >= 6 && x <= 9 {
		return true
	}
	return x == 10 && y >= 5 && y <= 10
}
