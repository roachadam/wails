//go:build windows && !server

package application

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Theme part bounds include transparent padding. Render the part over white and
// measure changed rows to size the replacement font glyph to its visible ink.

// themedInkHeight returns the height of the artwork a themed part draws, or 0
// when it cannot be measured.
func themedInkHeight(hwnd w32.HWND, part, state int32, width, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	hTheme := w32.OpenThemeData(hwnd, "Menu")
	if hTheme == 0 {
		return 0
	}
	defer w32.CloseThemeData(hTheme)

	dc := w32.CreateCompatibleDC(0)
	if dc == 0 {
		return 0
	}
	defer w32.DeleteDC(dc)

	var bmi w32.BITMAPINFO
	bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bmi.BmiHeader))
	bmi.BmiHeader.BiWidth = int32(width)
	bmi.BmiHeader.BiHeight = -int32(height) // top-down, so row 0 is the top
	bmi.BmiHeader.BiPlanes = 1
	bmi.BmiHeader.BiBitCount = 32
	bmi.BmiHeader.BiCompression = w32.BI_RGB

	var raw unsafe.Pointer
	bitmap := w32.CreateDIBSection(dc, &bmi, w32.DIB_RGB_COLORS, &raw, 0, 0)
	if bitmap == 0 || raw == nil {
		return 0
	}
	defer w32.DeleteObject(w32.HGDIOBJ(bitmap))

	old := w32.SelectObject(dc, w32.HGDIOBJ(bitmap))
	defer func() {
		if old != 0 {
			w32.SelectObject(dc, old)
		}
	}()

	// Opaque white: the alpha byte matters, because uxtheme composites these
	// parts with AlphaBlend and reads the destination's alpha as well.
	pixels := unsafe.Slice((*uint32)(raw), width*height)
	for i := range pixels {
		pixels[i] = 0xffffffff
	}

	rect := w32.RECT{Right: int32(width), Bottom: int32(height)}
	if !w32.DrawThemeBackground(hTheme, dc, part, state, &rect) {
		return 0
	}

	// A row counts as ink if any pixel in it moved off white at all. Partial
	// coverage at the very edge of an anti-aliased shape is still part of it.
	const white = 250
	first, last := -1, -1
	for y := range height {
		for x := range width {
			p := pixels[y*width+x]
			if p&0xff < white || (p>>8)&0xff < white || (p>>16)&0xff < white {
				if first < 0 {
					first = y
				}
				last = y
				break
			}
		}
	}
	if first < 0 {
		return 0
	}
	return last - first + 1
}
