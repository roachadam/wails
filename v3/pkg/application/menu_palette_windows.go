//go:build windows && !server

package application

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Reading the light palette off the visual style.
//
// The light colours were hardcoded, and measuring native popups showed all of
// them wrong: the popup surface is 242 on Windows 10 and 249 on Windows 11 where
// wails used pure white, and the plate behind a checkmark is a stronger blue
// than the wash that was guessed. No system colour matches either - COLOR_MENU
// is 240, close to Windows 10 and wrong on Windows 11 - so there is nothing to
// look them up from.
//
// They can be measured instead. DrawThemeBackground paints a part in the visual
// style's own colours, which is exactly what is wanted here: the ink is the
// answer rather than the problem. Rendering each part into a small bitmap and
// reading a pixel gives what Windows would have drawn, on any style, without a
// constant.
//
// Light only. Dark has no native reference - Win32 popup menus do not follow
// dark mode, which is the whole reason this file's caller owner-draws them - so
// that palette stays wails' own.

// sentinel is a colour no menu surface uses, so a part that declines to paint
// can be told apart from one that painted something pale.
const sentinel = 0xffff00ff // opaque magenta

// themedLightColours returns lightMenuColours with every surface the theme will
// answer for replaced by what it actually draws.
//
// Reports false when there is no visual style, leaving the built-in palette in
// place. Individual parts that decline are left alone rather than failing the
// whole palette, since a style may implement some and not others.
func themedLightColours(hwnd w32.HWND) (menuColours, bool) {
	hTheme := w32.OpenThemeData(hwnd, "Menu")
	if hTheme == 0 {
		return lightMenuColours, false
	}
	defer w32.CloseThemeData(hTheme)

	s, ok := newPartSampler(24, 24)
	if !ok {
		return lightMenuColours, false
	}
	defer s.release()

	colours := lightMenuColours

	// The popup surface. MENU_POPUPBACKGROUND is the part that fills it; some
	// styles leave it to the item instead, so the item's own normal state is
	// tried second.
	if c, ok := s.fill(hTheme, w32.MENU_POPUPBACKGROUND, 0); ok {
		colours.background = c
	} else if c, ok := s.fill(hTheme, w32.MENU_POPUPITEM, w32.MPI_NORMAL); ok {
		colours.background = c
	}

	// Text is asked for rather than sampled. Glyph pixels are anti-aliased
	// against whatever is behind them, so reading one back gives a blend rather
	// than the colour that was set.
	//
	// Read before the surfaces below, because the hot band is mixed from the
	// item's own ink and would otherwise be mixed from the built-in palette's.
	for _, t := range []struct {
		state int32
		into  *uint32
	}{
		{w32.MPI_NORMAL, &colours.text},
		{w32.MPI_HOT, &colours.selectedText},
		{w32.MPI_DISABLED, &colours.disabledText},
	} {
		if c, ok := w32.GetThemeColor(hTheme, w32.MENU_POPUPITEM, t.state, w32.TMT_TEXTCOLOR); ok {
			*t.into = uint32(c)
		}
	}

	// The plate behind a checkmark, and the band behind the item under the
	// pointer. Both are drawn over the surface rather than instead of it -
	// Windows 11's hot band is a translucent wash - so they are sampled over the
	// surface colour and the result is what Windows would actually have drawn
	// there. Priming with an arbitrary colour instead lets it show through: a
	// grey wash over magenta is purple.
	//
	// A part that leaves the surface untouched has nothing to contribute, which
	// is the right answer for the check plate on Windows 11, where a checked
	// item is a bare tick with no plate at all.
	colours.checkBackground = colours.background
	if c, ok := s.fillOver(hTheme, w32.MENU_POPUPCHECKBACKGROUND, w32.MCB_NORMAL, colours.background); ok {
		colours.checkBackground = c
	}

	// The hot band is the one colour the theme cannot be trusted for on Windows
	// 11. MENU_POPUPITEM still paints Windows 10's blue there, while a native
	// menu darkens the surface slightly instead - measured as 240 on a 249
	// surface, with no blue in it. The legacy MENU_* parts describe Windows 10's
	// style, which is the same reason their margins needed scaling; Windows 11
	// draws its menus from a newer stack that leaves them behind.
	if legacyMenuPartsAreStale() {
		colours.selectedBg = mix(colours.background, colours.text, 4)
	} else if c, ok := s.fillOver(hTheme, w32.MENU_POPUPITEM, w32.MPI_HOT, colours.background); ok {
		colours.selectedBg = c
	} else if c, ok := w32.GetThemeColor(hTheme, w32.MENU_POPUPITEM, w32.MPI_HOT, w32.TMT_FILLCOLOR); ok {
		colours.selectedBg = uint32(c)
	}

	// The divider. Unlike the others this part paints a thin rule and leaves the
	// rest alone, so the darkest pixel is the answer rather than the centre one.
	if c, ok := s.line(hTheme, w32.MENU_POPUPSEPARATOR, 0); ok {
		colours.separator = c
	}

	return colours, true
}

// partSampler is a small offscreen surface parts are drawn into and read back
// from.
type partSampler struct {
	width, height int
	dc            w32.HDC
	bitmap        w32.HBITMAP
	old           w32.HGDIOBJ
	pixels        []uint32
}

func newPartSampler(width, height int) (*partSampler, bool) {
	dc := w32.CreateCompatibleDC(0)
	if dc == 0 {
		return nil, false
	}

	var bmi w32.BITMAPINFO
	bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(bmi.BmiHeader))
	bmi.BmiHeader.BiWidth = int32(width)
	bmi.BmiHeader.BiHeight = -int32(height) // top-down
	bmi.BmiHeader.BiPlanes = 1
	bmi.BmiHeader.BiBitCount = 32
	bmi.BmiHeader.BiCompression = w32.BI_RGB

	var raw unsafe.Pointer
	bitmap := w32.CreateDIBSection(dc, &bmi, w32.DIB_RGB_COLORS, &raw, 0, 0)
	if bitmap == 0 || raw == nil {
		w32.DeleteDC(dc)
		return nil, false
	}

	return &partSampler{
		width:  width,
		height: height,
		dc:     dc,
		bitmap: bitmap,
		old:    w32.SelectObject(dc, w32.HGDIOBJ(bitmap)),
		pixels: unsafe.Slice((*uint32)(raw), width*height),
	}, true
}

// draw primes the surface and paints one part over it.
func (s *partSampler) draw(hTheme w32.HTHEME, part, state int32, prime uint32) bool {
	for i := range s.pixels {
		s.pixels[i] = prime
	}
	rect := w32.RECT{Right: int32(s.width), Bottom: int32(s.height)}
	return w32.DrawThemeBackground(hTheme, s.dc, part, state, &rect)
}

// fillOver returns what a part looks like drawn over base, which is the only
// meaningful answer for a part that paints translucently.
//
// Reports false when the part left base untouched, meaning it contributes
// nothing rather than that it failed.
func (s *partSampler) fillOver(hTheme w32.HTHEME, part, state int32, base uint32) (uint32, bool) {
	primed := toDIB(base)
	if !s.draw(hTheme, part, state, 0xff000000|primed) {
		return 0, false
	}
	centre := s.pixels[(s.height/2)*s.width+s.width/2]
	if centre&0x00ffffff == primed {
		return 0, false
	}
	return toColorRef(centre), true
}

// fill returns the colour a part covers its rect with.
func (s *partSampler) fill(hTheme w32.HTHEME, part, state int32) (uint32, bool) {
	if !s.draw(hTheme, part, state, sentinel) {
		return 0, false
	}
	// Compare the colour only. DrawThemeBackground touches the alpha byte even
	// when it paints nothing, so testing all 32 bits reports a part as having
	// painted when it has not - and the sentinel then gets used as a colour,
	// which is as visible as it sounds.
	centre := s.pixels[(s.height/2)*s.width+s.width/2]
	if centre&0x00ffffff == sentinel&0x00ffffff {
		return 0, false // the part declined to paint
	}
	return toColorRef(centre), true
}

// line returns the darkest colour a part painted, for parts that draw a rule
// rather than a fill.
func (s *partSampler) line(hTheme w32.HTHEME, part, state int32) (uint32, bool) {
	// Primed white rather than with the sentinel: the answer here is whichever
	// pixel is darkest, and magenta would win that comparison against a pale
	// grey rule.
	if !s.draw(hTheme, part, state, 0xffffffff) {
		return 0, false
	}

	darkest, best := uint32(0), 1<<31-1
	for _, p := range s.pixels {
		sum := int((p & 0xff) + ((p >> 8) & 0xff) + ((p >> 16) & 0xff)) //nolint:gosec // 0..765
		if sum < best {
			best, darkest = sum, p
		}
	}
	// Nothing was drawn if the surface is still white throughout.
	if best >= 250*3 {
		return 0, false
	}
	return toColorRef(darkest), true
}

// legacyMenuPartsAreStale reports whether this is a build whose Menu visual
// style no longer describes what Windows draws. Windows 11 is 22000 and up.
//
// This is the only place an OS check is made rather than a measurement, because
// there is nothing left to measure: the theme answers, and answers wrongly.
func legacyMenuPartsAreStale() bool {
	v, err := w32.GetWindowsVersionInfo()
	return err == nil && v.Build >= 22000
}

// mix blends percent of b into a, per channel, in COLORREF order.
func mix(a, b uint32, percent uint32) uint32 {
	ch := func(shift uint32) uint32 {
		x, y := (a>>shift)&0xff, (b>>shift)&0xff
		return (x*(100-percent) + y*percent) / 100
	}
	return ch(0) | ch(8)<<8 | ch(16)<<16
}

// toDIB is the inverse of toColorRef.
func toDIB(c uint32) uint32 {
	r := c & 0xff
	g := (c >> 8) & 0xff
	b := (c >> 16) & 0xff
	return b | g<<8 | r<<16
}

// toColorRef converts a 32bpp BI_RGB pixel, which reads back as 0x00RRGGBB, to
// a COLORREF, which is 0x00BBGGRR.
func toColorRef(p uint32) uint32 {
	r := (p >> 16) & 0xff
	g := (p >> 8) & 0xff
	b := p & 0xff
	return r | g<<8 | b<<16
}

func (s *partSampler) release() {
	if s == nil || s.dc == 0 {
		return
	}
	if s.old != 0 {
		w32.SelectObject(s.dc, s.old)
	}
	if s.bitmap != 0 {
		w32.DeleteObject(w32.HGDIOBJ(s.bitmap))
	}
	w32.DeleteDC(s.dc)
	*s = partSampler{}
}
