//go:build windows && !server

package application

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Owner-drawn popup menu items.
//
// Windows draws popup menu item text from the uxtheme theme, which follows the
// system light/dark setting. There is no API to force dark menu text while the
// OS is in light mode: on 1809 the app-level opt-in is AllowDarkModeForApp,
// which only means "follow the system", and SetPreferredAppMode's ForceDark does
// not exist before build 18334. So a window asking for Windows.Theme == Dark on
// a light-mode OS got a dark menu background painted by updateTheme with dark
// system text on top of it - unreadable.
//
// Owner-drawing the items bypasses uxtheme: wails supplies both the background
// and the text colour, so an explicit theme renders correctly on any build and
// any system setting.
//
// Everything about the layout is derived from the system rather than hardcoded,
// so items scale with the user's menu font and the window's DPI. See
// menuMetrics.

type menuColours struct {
	background uint32
	text       uint32
	// checkBackground is the panel behind a checkmark, which Windows draws as
	// MENU_POPUPCHECKBACKGROUND. Without it a checked item is much harder to
	// pick out.
	checkBackground uint32
	// checkFramed draws the check panel as an outline rather than a filled
	// block. High Contrast schemes do it that way, and filling it with the
	// highlight colour there is far too loud.
	checkFramed  bool
	disabledText uint32
	selectedBg   uint32
	selectedText uint32
	separator    uint32
}

// rgb packs a colour to COLORREF byte order (0x00BBGGRR).
func rgb(r, g, b uint32) uint32 {
	return r | g<<8 | b<<16
}

var darkMenuColours = menuColours{
	background:      rgb(33, 33, 33),
	text:            rgb(222, 222, 222),
	checkBackground: rgb(62, 62, 64),
	disabledText:    rgb(110, 110, 110),
	selectedBg:      rgb(62, 62, 64),
	selectedText:    rgb(255, 255, 255),
	separator:       rgb(60, 60, 60),
}

var lightMenuColours = menuColours{
	background:      rgb(255, 255, 255),
	text:            rgb(0, 0, 0),
	checkBackground: rgb(204, 232, 255),
	disabledText:    rgb(109, 109, 109),
	selectedBg:      rgb(204, 232, 255),
	selectedText:    rgb(0, 0, 0),
	separator:       rgb(215, 215, 215),
}

// menuMetrics holds the layout for one DPI.
//
// Geometry comes from the Menu visual style, not from GetSystemMetrics.
// GetSystemMetrics returns the classic pre-theme values - SM_CXMENUCHECK, for
// instance, is the size of the old checkmark bitmap, not the width of the gutter
// a themed menu reserves - and using them produces a menu that is uniformly
// tighter and narrower than the real thing. GetThemePartSize and
// GetThemeMargins on the "Menu" class return what Windows actually draws with.
//
// The theme supplies the measurements only. The pixels are still ours, because
// the entire purpose of owner-drawing here is to override the colours: calling
// DrawThemeBackground for the checkmark or the submenu arrow would paint them in
// the system theme's colours, which on a light system means a dark glyph on our
// dark background - the very bug being fixed.
//
// Every value falls back to a DPI-scaled system metric when the theme is
// unavailable, which is the case under the classic style and in High Contrast.
type menuMetrics struct {
	dpi w32.UINT

	font w32.HFONT
	// checkFont and submenuFont are Marlett at the sizes the theme reports for
	// the checkmark and the submenu arrow. Two fonts because those parts are
	// different sizes - 16px and 9px on the stock style.
	// Each glyph carries its own font and character, because the icon font and
	// the Marlett fallback use different codepoints and either may be in use.
	checkFont   w32.HFONT
	checkChar   string
	bulletFont  w32.HFONT
	bulletChar  string
	submenuFont w32.HFONT
	submenuChar string

	// Raw measurements, combined by finalise.
	checkWidth   int // MENU_POPUPCHECK size
	checkHeight  int
	checkMarginX int // MENU_POPUPCHECK content margins, the padding around the glyph
	checkMarginY int
	gutterGap    int // MENU_POPUPGUTTER width, between the check column and the label
	itemPadLeft  int // MENU_POPUPITEM content margins
	itemPadRight int
	itemPadY     int
	sepRule      int // MENU_POPUPSEPARATOR height
	textHeight   int // from the menu font

	// Derived layout.
	gutterWidth int
	// submenuWidth is reserved on the right of every item, as Windows does, so
	// the arrow never overlaps a long label. submenuGlyph is the arrow itself,
	// without the margins that make up the rest of that column.
	submenuWidth   int
	submenuGlyph   int
	submenuGlyphW  int
	submenuMarginY int
	minHeight      int
	sepHeight      int

	// labelReserveScaled is labelReserve in this DPI's units. See themeScale.
	labelReserveScaled int

	// lightPalette is the light-mode palette read off the visual style, and
	// havePalette whether it could be. See themedLightColours.
	lightPalette menuColours
	havePalette  bool

	// themed records whether the measurements came from the visual style.
	themed bool
}

// labelReserve is the space Windows keeps to the right of every popup label,
// before the submenu column.
//
// Windows adds no gap of its own between a label and its accelerator: a native
// item with an accelerator is exactly as wide as the same item without one plus
// the accelerator's text. The separation comes from this reserve, which is
// present on every item whether or not it has an accelerator, and on submenu
// items too.
//
// It is a constant. Measured against native popups on build 19045 it stayed at
// 27 across DPI 96 and 120 and menu fonts of -12, -15 and -18, so it follows
// neither the display scaling nor the font, and no theme part or system metric
// available here evaluates to it at both DPIs - gutterWidth+SM_CXEDGE and
// submenuWidth+submenuGlyph both give 27 at 96 DPI and neither does at 120.
// Deriving it would mean inventing a formula that happens to fit, so it is
// recorded as what it is.
const labelReserve = 27

// Marlett is the symbol font Windows draws menu glyphs from. Being a font it
// renders in whatever text colour is set, which is why it can supply Windows'
// own shapes without the colour problem that rules out DrawThemeBackground.
//
// The codes were read off a rendering of the whole font rather than recalled:
// 4 is the larger triangle used for scrollbars, 8 the smaller one menus use,
// and b and i are heavier variants of the check and the dot.
const (
	marlettFace    = "Marlett"
	marlettCheck   = "a" // 0x61, checkmark
	marlettBullet  = "h" // 0x68, filled dot for a radio item
	marlettSubmenu = "8" // 0x38, small right-pointing triangle
)

// Segoe MDL2 Assets is the icon font Windows 10 draws its own menu glyphs from.
// They are different shapes from Marlett's, not the same shapes at another
// size: a two-stroke chevron rather than a solid triangle, and a lighter check.
// Resizing Marlett was never going to match them.
//
// The codes were picked off a rendering of the font's E7xx block rather than
// recalled.
const (
	iconFace         = "Segoe MDL2 Assets"
	iconCheck        = "\ue73e" // CheckMark
	iconBullet       = "\ue7c8" // filled dot for a radio item
	iconChevronRight = "\ue76c" // ChevronRight, the submenu arrow
)

// newGlyphFont builds the icon font at the given size, falling back to Marlett
// when it cannot draw the character.
//
// The check has to be for the glyph, not the typeface. GDI reports back
// whatever face name was requested, so comparing names says nothing, and a font
// that exists but lacks the codepoint draws a missing-glyph box - which reads as
// a bug rather than a fallback.
func newGlyphFont(hwnd w32.HWND, height int, icon, marlett string) (w32.HFONT, string) {
	if font := newIconFont(height); font != 0 {
		if fontCanDraw(hwnd, font, icon) {
			return font, icon
		}
		w32.DeleteObject(w32.HGDIOBJ(font))
	}
	return newMarlettFont(height), marlett
}

// iconFontForInk builds the icon font at whatever size makes glyph's ink about
// target pixels tall.
//
// Sizing by the em box undershoots: an icon font pads its glyphs inside the em,
// so a font of height N draws an outline noticeably smaller than N. For the
// submenu arrow, whose part is only nine pixels at 96 DPI to begin with, that
// difference is the whole character - it comes out thin and grey where Windows'
// is solid.
//
// Falls back to sizing by the em box when the glyph cannot be measured.
func iconFontForInk(hwnd w32.HWND, glyph string, target int) w32.HFONT {
	if target <= 0 || glyph == "" {
		return 0
	}

	// Large enough that the ink height is a precise ratio rather than a handful
	// of pixels rounded to nothing.
	const probeEm = 64

	em := target
	if probe := newIconFont(probeEm); probe != 0 {
		if ink := glyphInk(hwnd, probe, glyph); ink > 0 {
			// Round to nearest rather than truncating: at these sizes one pixel
			// is a visible fraction of the glyph.
			em = (probeEm*target*2/ink + 1) / 2
		}
		w32.DeleteObject(w32.HGDIOBJ(probe))
	}
	return newIconFont(em)
}

func glyphInk(hwnd w32.HWND, font w32.HFONT, glyph string) int {
	hdc := w32.GetDC(hwnd)
	if hdc == 0 {
		return 0
	}
	defer w32.ReleaseDC(hwnd, hdc)

	old := w32.SelectObject(hdc, w32.HGDIOBJ(font))
	ink := w32.GlyphInkHeight(hdc, []rune(glyph)[0])
	if old != 0 {
		w32.SelectObject(hdc, old)
	}
	return ink
}

// contentBox is a themed part less its own margins, floored so a part that
// reports margins as large as itself still yields something drawable.
func contentBox(size, margin int) int {
	if inner := size - margin; inner > 0 {
		return inner
	}
	return size
}

// newIconFont builds Segoe MDL2 Assets at the given cell height. Unlike Marlett
// it is a normal-charset font whose glyphs fill their box, so the theme's part
// size is the right height to ask for.
func newIconFont(height int) w32.HFONT {
	if height <= 0 {
		return 0
	}
	lf := w32.LOGFONT{
		// Negative: match the character height rather than the cell height. A
		// positive value has the font mapper include internal leading, so the
		// glyph lands well under the requested size - which for an icon font,
		// whose glyphs are drawn to fill their em box, means it never reaches
		// the size the layout reserved for it.
		Height:  -int32(height),
		Weight:  400, // FW_NORMAL
		CharSet: 1,   // DEFAULT_CHARSET
		Quality: 5,   // CLEARTYPE_QUALITY
	}
	copy(lf.FaceName[:], w32.MustStringToUTF16(iconFace))
	return w32.CreateFontIndirect(&lf)
}

func fontCanDraw(hwnd w32.HWND, font w32.HFONT, s string) bool {
	hdc := w32.GetDC(hwnd)
	if hdc == 0 {
		return false
	}
	defer w32.ReleaseDC(hwnd, hdc)

	old := w32.SelectObject(hdc, w32.HGDIOBJ(font))
	ok := w32.FontHasGlyphs(hdc, s)
	if old != 0 {
		w32.SelectObject(hdc, old)
	}
	return ok
}

// newMarlettFont builds Marlett at the given cell height. SYMBOL_CHARSET
// matters: Marlett has no ANSI mapping, and requesting the wrong charset gets a
// substituted font with entirely different glyphs.
func newMarlettFont(height int) w32.HFONT {
	if height <= 0 {
		return 0
	}
	lf := w32.LOGFONT{
		Height:  int32(height),
		Weight:  400, // FW_NORMAL
		CharSet: 2,   // SYMBOL_CHARSET
		Quality: 5,   // CLEARTYPE_QUALITY
	}
	copy(lf.FaceName[:], w32.MustStringToUTF16(marlettFace))
	return w32.CreateFontIndirect(&lf)
}

// drawSymbol renders one Marlett glyph centred in rect using the given font.
func drawSymbol(hdc w32.HDC, font w32.HFONT, glyph string, rect w32.RECT) {
	if font == 0 {
		return
	}
	old := w32.SelectObject(hdc, w32.HGDIOBJ(font))
	drawMenuString(hdc, glyph, &rect, w32.DT_CENTER|w32.DT_SINGLELINE|w32.DT_VCENTER)
	if old != 0 {
		w32.SelectObject(hdc, old)
	}
}

// menuFontForDpi returns the user's menu font for the given DPI.
//
// GetStockObject is deliberately not used, and not because of the bug that made
// it return 0: DEFAULT_GUI_FONT is the legacy default GUI font, not the menu
// font, so it would be the wrong typeface even once that is fixed. The menu font
// belongs to NONCLIENTMETRICS.
//
// Returns 0 if the metrics are unavailable, which callers treat as "leave the
// device context's current font alone" rather than substituting a guess.
func menuFontForDpi(dpi w32.UINT) w32.HFONT {
	ncm, ok := w32.GetNonClientMetricsForDpi(dpi)
	if !ok {
		return 0
	}
	lf := ncm.MenuFont
	return w32.CreateFontIndirect(&lf)
}

// newMenuMetrics derives the layout for hwnd's current DPI.
func newMenuMetrics(hwnd w32.HWND) *menuMetrics {
	dpi := w32.GetDpiForWindow(hwnd)
	m := &menuMetrics{dpi: dpi, font: menuFontForDpi(dpi)}

	hdc := w32.GetDC(hwnd)
	if hdc != 0 {
		var oldFont w32.HGDIOBJ
		if m.font != 0 {
			oldFont = w32.SelectObject(hdc, w32.HGDIOBJ(m.font))
		}

		m.applyThemeMetrics(hwnd, hdc)
		m.applyFontMetrics(hdc)

		if oldFont != 0 {
			w32.SelectObject(hdc, oldFont)
		}
		w32.ReleaseDC(hwnd, hdc)
	}

	m.applyFallbacks()
	m.finalise()

	// Sampled here so it happens once per metrics rebuild rather than once per
	// drawn item - a popup raises WM_DRAWITEM for every item on every repaint.
	m.lightPalette, m.havePalette = themedLightColours(hwnd)

	// Sized from the theme's own part sizes, so the glyphs scale with the menu
	// rather than with the label font.
	// Sized to the part's content box - the part less its own margins - rather
	// than to the part. The theme pads its artwork, so the ink inside a 16px
	// checkmark part is nearer 10px, and an icon glyph drawn at the full 16
	// comes out visibly larger than the one Windows draws. The submenu part is
	// barely padded, so its arrow gets close to the full size instead.
	m.checkFont, m.checkChar = newGlyphFont(hwnd, contentBox(m.checkHeight, m.checkMarginY), iconCheck, marlettCheck)
	m.bulletFont, m.bulletChar = newGlyphFont(hwnd, contentBox(m.checkHeight, m.checkMarginY), iconBullet, marlettBullet)
	// The arrow is sized by its ink rather than by the em box. Its part carries
	// almost no margin, so the target is the part itself, and at nine pixels the
	// shortfall from em-box sizing is most of the glyph. The check and the
	// bullet keep em-box sizing: their part's margins already describe the
	// padding, so subtracting them lands in the right place.
	// The target is the height of the artwork Windows actually draws, measured
	// rather than assumed: the part's box is taller than the chevron inside it,
	// so sizing to the box overshoots. Falls back to the box when the part
	// cannot be measured, which is the case without a visual style.
	arrowInk := themedInkHeight(hwnd, w32.MENU_POPUPSUBMENU, w32.MSM_NORMAL, m.submenuGlyphW, m.submenuGlyph)
	if arrowInk <= 0 {
		arrowInk = contentBox(m.submenuGlyph, m.submenuMarginY)
	}
	if font := iconFontForInk(hwnd, iconChevronRight, arrowInk); font != 0 && fontCanDraw(hwnd, font, iconChevronRight) {
		m.submenuFont, m.submenuChar = font, iconChevronRight
	} else {
		if font != 0 {
			w32.DeleteObject(w32.HGDIOBJ(font))
		}
		m.submenuFont, m.submenuChar = newMarlettFont(m.submenuGlyph), marlettSubmenu
	}

	return m
}

// applyThemeMetrics reads the raw geometry the visual style defines. The values
// are combined in finalise, because the item height depends on the text height
// too and that is not known until the font has been measured.
//
// Every formula below was checked against a native popup on build 17763, read
// back with GetMenuItemRect rather than estimated: item height 22, separator 7,
// with a 15px menu font.
func (m *menuMetrics) applyThemeMetrics(hwnd w32.HWND, hdc w32.HDC) {
	hTheme := w32.OpenThemeData(hwnd, "Menu")
	if hTheme == 0 {
		return
	}
	defer w32.CloseThemeData(hTheme)

	m.themed = true

	if sz, ok := w32.GetThemePartSize(hTheme, hdc, w32.MENU_POPUPCHECK, w32.MC_CHECKMARKNORMAL, w32.TS_TRUE); ok {
		m.checkWidth, m.checkHeight = int(sz.CX), int(sz.CY)
	}
	// The checkmark's own margins are the padding around the glyph - (3,3,3,3)
	// on the stock style. MENU_POPUPCHECKBACKGROUND's margins are not: they are
	// (0,6,0,0), so reading the vertical padding from there yields zero and the
	// rows come out too short.
	if mg, ok := w32.GetThemeMargins(hTheme, hdc, w32.MENU_POPUPCHECK, w32.MC_CHECKMARKNORMAL, w32.TMT_CONTENTMARGINS); ok {
		m.checkMarginX = int(mg.CxLeftWidth + mg.CxRightWidth)
		m.checkMarginY = int(mg.CyTopHeight + mg.CyBottomHeight)
	}

	if sz, ok := w32.GetThemePartSize(hTheme, hdc, w32.MENU_POPUPGUTTER, 0, w32.TS_TRUE); ok {
		m.gutterGap = int(sz.CX)
	}

	if mg, ok := w32.GetThemeMargins(hTheme, hdc, w32.MENU_POPUPITEM, w32.MPI_NORMAL, w32.TMT_CONTENTMARGINS); ok {
		m.itemPadLeft, m.itemPadRight = int(mg.CxLeftWidth), int(mg.CxRightWidth)
		m.itemPadY = int(mg.CyTopHeight + mg.CyBottomHeight)
	}

	// Windows reserves the submenu column on every item, not just the ones with
	// a submenu, which is why a native popup is wider than its longest label.
	// The arrow's margins are part of that column.
	if sz, ok := w32.GetThemePartSize(hTheme, hdc, w32.MENU_POPUPSUBMENU, w32.MSM_NORMAL, w32.TS_TRUE); ok {
		m.submenuWidth = int(sz.CX)
		m.submenuGlyphW = int(sz.CX)
		m.submenuGlyph = int(sz.CY)
	}
	if mg, ok := w32.GetThemeMargins(hTheme, hdc, w32.MENU_POPUPSUBMENU, w32.MSM_NORMAL, w32.TMT_CONTENTMARGINS); ok {
		m.submenuWidth += int(mg.CxLeftWidth + mg.CxRightWidth)
		m.submenuMarginY = int(mg.CyTopHeight + mg.CyBottomHeight)
	}

	if sz, ok := w32.GetThemePartSize(hTheme, hdc, w32.MENU_POPUPSEPARATOR, 0, w32.TS_TRUE); ok {
		m.sepRule = int(sz.CY)
	}

	// Everything the theme expresses in its own units has to be scaled to the
	// artwork it is actually drawing with. GetThemePartSize follows the display
	// scaling; GetThemeMargins does not - it answers in the units of the 96 DPI
	// artwork whatever DPI is asked for, including through OpenThemeDataForDpi.
	// Laying a menu out from both leaves every item several pixels too tight at
	// 150% and 200%.
	//
	// The factor is how much the artwork grew, not dpi/96, and those differ. At
	// 125% on Windows 10 the check part goes 16 to 18 and its margins genuinely
	// stay at 6; at 150% on Windows 11 it goes 16 to 25 and they become 10; at
	// 200% it goes 16 to 32 and they become 12. All measured against native
	// popups.
	// Only the vertical margins and the reserve. Scaling the horizontal ones
	// too made every popup wider than native by exactly their contribution -
	// 12px at 200%, being checkMarginX 6 to 12 and the item padding 3 to 6 on
	// each side. Windows evidently applies the artwork scale down the item and
	// not across it. Measured on Windows 11 at 150% and 200%.
	scale := themeScale(hwnd, hdc, m.checkHeight)
	m.checkMarginY = scale.apply(m.checkMarginY)
	m.itemPadY = scale.apply(m.itemPadY)
	m.labelReserveScaled = scale.apply(labelReserve)
}

// themeRatio is the factor between the artwork the theme draws with and the
// artwork its margins are expressed in. Kept as a fraction rather than a float
// so the arithmetic is exact and truncates the way the measurements did.
type themeRatio struct{ have, base int }

func (r themeRatio) apply(v int) int {
	if r.base <= 0 {
		return v
	}
	return v * r.have / r.base
}

// themeScale measures that factor by asking for the same part at 96 DPI.
// OpenThemeDataForDpi reports 96 DPI sizes reliably even from a scaled window,
// which is what makes it usable as a reference - it is only its margins that
// ignore the request.
//
// Returns 1:1 when the DPI-aware entry point is missing, leaving the metrics as
// the theme reported them.
func themeScale(hwnd w32.HWND, hdc w32.HDC, have int) themeRatio {
	base := w32.OpenThemeDataForDpi(hwnd, "Menu", w32.USER_DEFAULT_SCREEN_DPI)
	if base == 0 {
		return themeRatio{1, 1}
	}
	defer w32.CloseThemeData(base)

	sz, ok := w32.GetThemePartSize(base, hdc, w32.MENU_POPUPCHECK, w32.MC_CHECKMARKNORMAL, w32.TS_TRUE)
	if !ok || sz.CY <= 0 {
		return themeRatio{1, 1}
	}
	return themeRatio{have, int(sz.CY)}
}

// applyFontMetrics records what depends on the menu font. hdc must already have
// that font selected.
func (m *menuMetrics) applyFontMetrics(hdc w32.HDC) {
	var tm w32.TEXTMETRIC
	if !w32.GetTextMetrics(hdc, &tm) {
		return
	}
	m.textHeight = int(tm.TmHeight + tm.TmExternalLeading)
}

// finalise combines the theme and font measurements into the layout.
//
//	gutter      = check + its margins + the gutter column
//	item height = the taller of the checkmark block and the text block
//	separator   = the rule plus the item's vertical padding
//
// On build 17763 that gives 22 and 7, matching the native menu exactly.
func (m *menuMetrics) finalise() {
	m.gutterWidth = m.checkWidth + m.checkMarginX + m.gutterGap
	m.minHeight = max(m.checkHeight+m.checkMarginY, m.textHeight+m.itemPadY)
	m.sepHeight = m.sepRule + m.itemPadY
}

// applyFallbacks covers the classic style, High Contrast, and the case where the
// device context could not be obtained. Every fallback is a DPI-scaled system
// metric rather than a fixed pixel count, so it still follows display scaling -
// it is simply the classic geometry rather than the themed geometry.
func (m *menuMetrics) applyFallbacks() {
	edgeX := w32.SystemMetricForDpi(w32.SM_CXEDGE, m.dpi)
	edgeY := w32.SystemMetricForDpi(w32.SM_CYEDGE, m.dpi)

	if m.checkWidth <= 0 {
		m.checkWidth = w32.SystemMetricForDpi(w32.SM_CXMENUCHECK, m.dpi)
	}
	if m.checkHeight <= 0 {
		m.checkHeight = w32.SystemMetricForDpi(w32.SM_CYMENUCHECK, m.dpi)
	}
	if m.checkMarginX <= 0 {
		m.checkMarginX = 2 * edgeX
	}
	if m.checkMarginY <= 0 {
		m.checkMarginY = 2 * edgeY
	}
	if m.gutterGap <= 0 {
		m.gutterGap = edgeX
	}
	if m.itemPadLeft <= 0 && !m.themed {
		m.itemPadLeft = edgeX
	}
	if m.itemPadRight <= 0 && !m.themed {
		m.itemPadRight = edgeX
	}
	if m.itemPadY <= 0 && !m.themed {
		m.itemPadY = 2 * edgeY
	}
	if m.submenuWidth <= 0 {
		m.submenuWidth = w32.SystemMetricForDpi(w32.SM_CXMENUSIZE, m.dpi)
	}
	if m.submenuGlyph <= 0 {
		m.submenuGlyph = w32.SystemMetricForDpi(w32.SM_CYMENUSIZE, m.dpi)
	}
	if m.textHeight <= 0 {
		m.textHeight = w32.SystemMetricForDpi(w32.SM_CYMENU, m.dpi)
	}
	if m.sepRule <= 0 {
		m.sepRule = 2*w32.SystemMetricForDpi(w32.SM_CYBORDER, m.dpi) + 1
	}
}

// reserve is labelReserve in this DPI's units.
func (m *menuMetrics) reserve() int {
	if m.labelReserveScaled > 0 {
		return m.labelReserveScaled
	}
	return labelReserve
}

// textLeft is where an item's label starts.
func (m *menuMetrics) textLeft() int { return m.gutterWidth + m.itemPadLeft }

func (m *menuMetrics) release() {
	if m == nil {
		return
	}
	for _, f := range []*w32.HFONT{&m.font, &m.checkFont, &m.bulletFont, &m.submenuFont} {
		if *f != 0 {
			w32.DeleteObject(w32.HGDIOBJ(*f))
			*f = 0
		}
	}
}

// currentMenuMetrics returns the cached metrics, rebuilding them if the window
// has moved to a display with a different DPI.
//
// The font is a GDI object, so it is built once per DPI rather than per drawn
// item - a menu raises WM_DRAWITEM for every item on every repaint.
func (w *windowsWebviewWindow) currentMenuMetrics() *menuMetrics {
	dpi := w32.GetDpiForWindow(w.hwnd)
	if w.menuMetrics != nil && w.menuMetrics.dpi == dpi {
		return w.menuMetrics
	}
	w.menuMetrics.release()
	w.menuMetrics = newMenuMetrics(w.hwnd)
	return w.menuMetrics
}

// menuItemFor resolves an owner-drawn item from the menu's draw mapping, keyed
// by the identifier Windows reports in WM_DRAWITEM and WM_MEASUREITEM.
//
// Two mappings that look interchangeable are not. menuMapping is keyed by
// command id for WM_COMMAND dispatch; drawMapping is keyed by whatever was
// handed to AppendMenu, which for an MF_POPUP item is the submenu's HMENU
// instead. Using menuMapping here silently fails to resolve every submenu item.
//
// The global menuItemMap is not usable either: Menu.AddSeparator never registers
// its item, so getMenuItemByID returns nil for every separator. drawMapping is
// populated for all items, separators included, which lets separators be
// identified explicitly rather than inferred from a failed lookup.
// It resolves only items wails actually owner-draws. A menu bar item is present
// in drawMapping too, but it is drawn by MenuBarWndProc, so claiming it here
// would starve that handler and paint the bar with popup styling.
func (w *windowsWebviewWindow) menuItemFor(itemID uint32) (*MenuItem, bool) {
	if w.menu == nil || w.menu.drawMapping == nil {
		return nil, false
	}
	item, ok := w.menu.drawMapping[int(itemID)]
	if !ok || item == nil {
		return nil, false
	}
	if impl, ok := item.impl.(*windowsMenuItem); !ok || !impl.ownerDraw {
		return nil, false
	}
	return item, true
}

func menuItemLabelAccel(item *MenuItem) (label string, accel string) {
	label = item.label
	if item.accelerator != nil {
		accel = item.accelerator.String()
	}
	return label, accel
}

// withMenuFont selects the menu font for the duration of fn. It never hands
// SelectObject a 0 handle, which would panic.
func (m *menuMetrics) withMenuFont(hdc w32.HDC, fn func()) {
	if m.font == 0 {
		fn()
		return
	}
	old := w32.SelectObject(hdc, w32.HGDIOBJ(m.font))
	fn()
	if old != 0 {
		w32.SelectObject(hdc, old)
	}
}

// widestAccelerator returns the width of the widest accelerator in the popup
// that item belongs to, or 0 if none of them has one.
//
// Windows sizes the accelerator column once for the whole popup so the
// accelerators line up, and reserves it on every item including those without
// one. Measuring each item against only its own accelerator makes the popup as
// wide as its longest label and no wider, which leaves the accelerators nowhere
// to sit - a 63px shortfall against the native menu in the case measured here.
//
// hdc must already have the menu font selected.
func widestAccelerator(hdc w32.HDC, item *MenuItem) int {
	impl, ok := item.impl.(*windowsMenuItem)
	if !ok || impl.parent == nil {
		return 0
	}

	widest := 0
	for _, sibling := range impl.parent.items {
		// Hidden items are skipped by buildMenuLevel before AppendMenu, so they
		// occupy no row; reserving width for an accelerator that will never be
		// drawn would just widen the popup.
		if sibling.accelerator == nil || sibling.Hidden() {
			continue
		}
		if w, _ := measureText(hdc, sibling.accelerator.String()); w > widest {
			widest = w
		}
	}
	return widest
}

func measureText(hdc w32.HDC, s string) (int, int) {
	if s == "" {
		return 0, 0
	}
	var sz w32.SIZE
	if !w32.GetTextExtentPoint32(hdc, w32.MustStringToUTF16Ptr(s), len([]rune(s)), &sz) {
		return 0, 0
	}
	return int(sz.CX), int(sz.CY)
}

func drawMenuString(hdc w32.HDC, s string, rect *w32.RECT, flags uint32) {
	if s == "" {
		return
	}
	w32.DrawText(hdc, w32.MustStringToUTF16(s), -1, rect, flags)
}

// systemMenuColours reads the palette from the system rather than using either
// built-in one.
//
// This is what High Contrast needs. Those schemes are an accessibility setting -
// the user has chosen specific colours, often for legibility reasons - so
// painting our own dark or light palette over them defeats the point, and can
// produce combinations with no contrast at all. updateTheme already declines to
// theme anything when High Contrast is on; owner-draw has to do the same.
func systemMenuColours() menuColours {
	sys := func(index int) uint32 { return uint32(w32.GetSysColor(index)) }
	return menuColours{
		background: sys(w32.COLOR_MENU),
		text:       sys(w32.COLOR_MENUTEXT),
		// Outlined in the text colour, the way these schemes draw it. Filling
		// it with COLOR_HIGHLIGHT produced a solid block that read as selected.
		checkBackground: sys(w32.COLOR_MENUTEXT),
		checkFramed:     true,
		disabledText:    sys(w32.COLOR_GRAYTEXT),
		selectedBg:      sys(w32.COLOR_HIGHLIGHT),
		selectedText:    sys(w32.COLOR_HIGHLIGHTTEXT),
		separator:       sys(w32.COLOR_BTNSHADOW),
	}
}

func (w *windowsWebviewWindow) menuColours() menuColours {
	if w32.IsCurrentlyHighContrastMode() {
		return systemMenuColours()
	}
	if w.menuOwnerDrawDark {
		return darkMenuColours
	}
	// Light mode has a native menu to match, so the surfaces come from the
	// visual style rather than from the built-in palette. Dark mode has no such
	// reference, since Win32 popups do not follow dark mode.
	if m := w.currentMenuMetrics(); m != nil && m.havePalette {
		return m.lightPalette
	}
	return lightMenuColours
}

// drawCheck paints the checkmark and the background panel behind it, centred in
// the gutter at the size the theme reports.
//
// Windows draws MENU_POPUPCHECK inside MENU_POPUPCHECKBACKGROUND, and the panel
// is what makes a checked item read as checked at a glance. DrawThemeBackground
// is not used for either: it would paint them in the system theme's colours,
// which on a light system means a dark glyph on our dark background.
func (m *menuMetrics) drawCheck(hdc w32.HDC, rect w32.RECT, colours menuColours, selected, disabled, radio bool) {
	height := int(rect.Bottom - rect.Top)

	// Centre the glyph in the check column rather than pinning it to the left
	// edge. checkMarginX is the padding either side of it, and the item's own
	// left padding is 0 under the visual style, so without this the tick sits
	// against the popup border.
	left := m.itemPadLeft + m.checkMarginX/2
	top := max((height-m.checkHeight)/2, 0)

	panel := w32.RECT{
		Left:   rect.Left + int32(left),
		Right:  rect.Left + int32(left+m.checkWidth),
		Top:    rect.Top + int32(top),
		Bottom: rect.Top + int32(top+m.checkHeight),
	}

	if !selected {
		panelBrush := w32.CreateSolidBrush(colours.checkBackground)
		if colours.checkFramed {
			w32.FrameRect(hdc, &panel, panelBrush)
		} else {
			w32.FillRect(hdc, &panel, panelBrush)
		}
		w32.DeleteObject(w32.HGDIOBJ(panelBrush))
	}

	font, glyph := m.checkFont, m.checkChar
	if radio {
		font, glyph = m.bulletFont, m.bulletChar
	}

	previous := w32.SetTextColor(hdc, w32.COLORREF(colours.text))
	if disabled {
		w32.SetTextColor(hdc, w32.COLORREF(colours.disabledText))
	} else if selected {
		w32.SetTextColor(hdc, w32.COLORREF(colours.selectedText))
	}
	drawSymbol(hdc, font, glyph, panel)
	w32.SetTextColor(hdc, previous)
}

// handleMeasureMenuItem sizes an owner-drawn popup item. Without this, Windows
// has no size for the item and renders it collapsed.
func (w *windowsWebviewWindow) handleMeasureMenuItem(lparam uintptr) bool {
	mis := (*w32.MEASUREITEMSTRUCT)(unsafe.Pointer(lparam))
	if mis.CtlType != w32.ODT_MENU {
		return false
	}

	// Not one of ours: leave the message for whoever owns it rather than
	// reporting a made-up size.
	item, ok := w.menuItemFor(mis.ItemID)
	if !ok {
		return false
	}

	metrics := w.currentMenuMetrics()

	if item.IsSeparator() {
		mis.ItemHeight = uint32(metrics.sepHeight)
		mis.ItemWidth = 0
		return true
	}

	label, _ := menuItemLabelAccel(item)

	hdc := w32.GetDC(w.hwnd)
	if hdc == 0 {
		mis.ItemHeight = uint32(metrics.minHeight)
		mis.ItemWidth = uint32(metrics.textLeft())
		return true
	}
	var labelW, labelH, accelW int
	metrics.withMenuFont(hdc, func() {
		labelW, labelH = measureText(hdc, label)
		// The accelerator column is sized by the widest accelerator anywhere in
		// this popup, not by this item's own. Windows reserves it on every item
		// so the accelerators line up in a column; measuring per item makes the
		// popup as wide as its longest label alone, and accelerators then have
		// nowhere to sit.
		accelW = widestAccelerator(hdc, item)
	})
	w32.ReleaseDC(w.hwnd, hdc)

	// The submenu column is likewise reserved on every item, so an arrow can
	// never be drawn over a label that was measured without it.
	// labelReserve goes on unconditionally - Windows keeps it whether or not
	// there is an accelerator to put in it, so a popup with none is otherwise
	// too narrow by exactly that much.
	width := metrics.textLeft() + labelW + metrics.itemPadRight + metrics.submenuWidth + metrics.reserve()
	if accelW > 0 {
		width += accelW
	}
	height := max(labelH+metrics.itemPadY, metrics.minHeight)

	mis.ItemWidth = uint32(width)
	mis.ItemHeight = uint32(height)
	return true
}

// handleDrawMenuItem paints an owner-drawn popup item: background, label,
// accelerator, checkmark, submenu arrow and separator, in wails-chosen colours.
func (w *windowsWebviewWindow) handleDrawMenuItem(lparam uintptr) bool {
	dis := (*w32.DRAWITEMSTRUCT)(unsafe.Pointer(lparam))
	if dis.ControlType != w32.ODT_MENU {
		return false
	}

	// Not one of ours - a menu bar item, or a menu belonging to another
	// Win32Menu. Decline so MenuBarWndProc still sees it.
	item, ok := w.menuItemFor(dis.ItemID)
	if !ok {
		return false
	}

	metrics := w.currentMenuMetrics()
	colours := w.menuColours()
	rect := dis.RcItem

	label, accel := menuItemLabelAccel(item)
	hasSubmenu := item.submenu != nil
	isSeparator := item.IsSeparator()

	selected := dis.ItemState&w32.ODS_SELECTED != 0
	disabled := dis.ItemState&(w32.ODS_GRAYED|w32.ODS_DISABLED) != 0

	bg := colours.background
	if selected && !disabled && !isSeparator {
		bg = colours.selectedBg
	}
	bgBrush := w32.CreateSolidBrush(bg)
	w32.FillRect(dis.HDC, &rect, bgBrush)
	w32.DeleteObject(w32.HGDIOBJ(bgBrush))

	if isSeparator {
		lineBrush := w32.CreateSolidBrush(colours.separator)
		mid := rect.Top + (rect.Bottom-rect.Top)/2
		// A separator spans the text column only, starting right of the gutter,
		// the way MENU_POPUPSEPARATOR is drawn. Running it the full width of the
		// popup is one of the things that reads as not-native.
		line := w32.RECT{
			Left:   rect.Left + int32(metrics.gutterWidth),
			Top:    mid,
			Right:  rect.Right - int32(metrics.itemPadRight),
			Bottom: mid + int32(w32.SystemMetricForDpi(w32.SM_CYBORDER, metrics.dpi)),
		}
		w32.FillRect(dis.HDC, &line, lineBrush)
		w32.DeleteObject(w32.HGDIOBJ(lineBrush))
		return true
	}

	textColour := colours.text
	if disabled {
		textColour = colours.disabledText
	} else if selected {
		textColour = colours.selectedText
	}

	// dis.HDC belongs to Windows' menu painting, so every attribute changed here
	// is restored before returning - not just the font.
	var oldFont w32.HGDIOBJ
	if metrics.font != 0 {
		oldFont = w32.SelectObject(dis.HDC, w32.HGDIOBJ(metrics.font))
	}
	oldBkMode := w32.SetBkMode(dis.HDC, w32.TRANSPARENT)
	oldTextColour := w32.SetTextColor(dis.HDC, w32.COLORREF(textColour))

	if dis.ItemState&w32.ODS_CHECKED != 0 {
		metrics.drawCheck(dis.HDC, rect, colours, selected, disabled, item.IsRadio())
	}

	// The label occupies the text column: right of the gutter, left of the
	// reserved submenu column.
	labelRect := rect
	labelRect.Left += int32(metrics.textLeft())
	labelRect.Right -= int32(metrics.itemPadRight + metrics.submenuWidth)
	drawMenuString(dis.HDC, label, &labelRect, w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)

	// No arrow is drawn here. Windows draws the submenu arrow itself, after
	// WM_DRAWITEM returns, so anything painted in that column is covered a
	// moment later - verified by deleting this drawing entirely and watching the
	// arrow still appear. It uses the classic DFCS_MENUARROW triangle rather
	// than the theme's chevron because owner-draw bypasses the themed path,
	// which is a difference from a native popup that owner-draw cannot close.
	//
	// The column is still reserved in handleMeasureMenuItem, because Windows
	// reserves it too and the widths only match native when it is.
	//
	// What can be controlled is its colour: the arrow is a monochrome bitmap
	// blit, which takes the device context's text colour. Leaving that set to
	// the item's ink rather than restoring it is what stops the arrow coming out
	// in the system's menu text colour - dark, and close to invisible, on a dark
	// background.
	if !hasSubmenu && accel != "" {
		// Drawn in the item's own text colour. Windows does not dim
		// accelerators - using the disabled colour made them look disabled,
		// which is obvious under a High Contrast scheme where that colour is
		// green rather than a slightly lighter grey.
		accelRect := labelRect
		drawMenuString(dis.HDC, accel, &accelRect, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	if !hasSubmenu {
		w32.SetTextColor(dis.HDC, oldTextColour)
	}
	w32.SetBkMode(dis.HDC, oldBkMode)
	if oldFont != 0 {
		w32.SelectObject(dis.HDC, oldFont)
	}
	return true
}
