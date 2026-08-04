//go:build windows && !server

package application

import (
	"fmt"
	"unicode"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Popup items are owner-drawn so their text follows the window theme rather
// than the process-wide system theme. Layout still comes from Windows metrics.

// ownerDrawMenuData is native memory referenced by MENUITEMINFO.DwItemData.
// Windows retains this pointer, so neither the structure nor its text may live
// on the Go heap. MSAAMENUINFO also exposes the label to accessibility tools.
type ownerDrawMenuData struct {
	infoMemory w32.HGLOBAL
	textMemory w32.HGLOBAL
	text       string
}

type ownerDrawMenuEntry struct {
	item *MenuItem
	data *ownerDrawMenuData
}

func newOwnerDrawMenuData(text string) (*ownerDrawMenuData, error) {
	d := &ownerDrawMenuData{}
	infoSize := unsafe.Sizeof(w32.MSAAMENUINFO{})
	if infoSize > uintptr(^uint32(0)) {
		return nil, fmt.Errorf("MSAAMENUINFO is too large")
	}
	d.infoMemory = w32.TryGlobalAlloc(0, uint32(infoSize))
	if d.infoMemory == 0 {
		return nil, fmt.Errorf("GlobalAlloc failed for MSAAMENUINFO")
	}
	if err := d.setText(text); err != nil {
		d.release()
		return nil, err
	}
	return d, nil
}

func allocNativeMenuText(text string) (w32.HGLOBAL, *uint16, uint32, error) {
	chars := w32.MustStringToUTF16(text)
	byteCount := uint64(len(chars)) * uint64(unsafe.Sizeof(uint16(0)))
	if byteCount > uint64(^uint32(0)) {
		return 0, nil, 0, fmt.Errorf("menu label is too large")
	}
	memory := w32.TryGlobalAlloc(0, uint32(byteCount))
	if memory == 0 {
		return 0, nil, 0, fmt.Errorf("GlobalAlloc failed for menu label")
	}
	destination := unsafe.Slice((*uint16)(unsafe.Pointer(memory)), len(chars))
	copy(destination, chars)
	return memory, &destination[0], uint32(len(chars) - 1), nil
}

func (d *ownerDrawMenuData) setText(text string) error {
	if d == nil || d.infoMemory == 0 {
		return fmt.Errorf("owner-draw menu data is not allocated")
	}
	if d.textMemory != 0 && d.text == text {
		return nil
	}
	memory, pointer, length, err := allocNativeMenuText(text)
	if err != nil {
		return err
	}
	info := (*w32.MSAAMENUINFO)(unsafe.Pointer(d.infoMemory))
	oldText := d.textMemory
	info.DwMSAASignature = w32.MSAA_MENU_SIG
	info.CchWText = length
	info.PszWText = pointer
	d.textMemory = memory
	d.text = text
	if oldText != 0 {
		w32.GlobalFree(oldText)
	}
	return nil
}

func (d *ownerDrawMenuData) itemData() uintptr {
	if d == nil {
		return 0
	}
	return uintptr(d.infoMemory)
}

func (d *ownerDrawMenuData) release() {
	if d == nil {
		return
	}
	if d.infoMemory != 0 {
		w32.GlobalFree(d.infoMemory)
	}
	if d.textMemory != 0 {
		w32.GlobalFree(d.textMemory)
	}
	*d = ownerDrawMenuData{}
}

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

// menuMetrics holds visual-style geometry for one DPI. System metrics are used
// only when themes are unavailable because their classic menu sizes are smaller.
type menuMetrics struct {
	dpi w32.UINT

	font w32.HFONT
	// Glyph fonts may be Segoe MDL2 Assets or the Marlett fallback.
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
	// submenuWidth is reserved on every row; submenuGlyph is the arrow itself.
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

// labelReserve is the native gap between a label/accelerator and the submenu
// column. No theme property exposes it; native measurements keep it at 27px.
const labelReserve = 27

// Marlett provides recolourable fallbacks for the menu glyphs.
const (
	marlettFace    = "Marlett"
	marlettCheck   = "a" // 0x61, checkmark
	marlettBullet  = "h" // 0x68, filled dot for a radio item
	marlettSubmenu = "8" // 0x38, small right-pointing triangle
)

// Segoe MDL2 Assets supplies the modern check, bullet, and chevron shapes.
const (
	iconFace         = "Segoe MDL2 Assets"
	iconCheck        = "\ue73e" // CheckMark
	iconBullet       = "\ue7c8" // filled dot for a radio item
	iconChevronRight = "\ue76c" // ChevronRight, the submenu arrow
)

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
			// Truncate rather than round to nearest. Rounding up put the ink a
			// pixel over the artwork's, which reads as a heavier tick and a
			// noticeably larger radio dot beside a native menu.
			em = probeEm * target / ink
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

// newInkGlyph builds the icon font sized so glyph's ink is about target tall,
// falling back to Marlett at that size when the icon font cannot draw it.
//
// The font iconFontForInk returns is used directly. An earlier version measured
// its em and rebuilt from that, which lost a pixel or two to rounding and left
// the checkmark visibly smaller than the one Windows draws.
func newInkGlyph(hwnd w32.HWND, target int, icon, marlett string) (w32.HFONT, string) {
	if font := iconFontForInk(hwnd, icon, target); font != 0 {
		if fontCanDraw(hwnd, font, icon) {
			return font, icon
		}
		w32.DeleteObject(w32.HGDIOBJ(font))
	}
	return newMarlettFont(target), marlett
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

	// Theme part boxes include padding, so size glyph fonts from rendered ink.
	checkInk := themedInkHeight(hwnd, w32.MENU_POPUPCHECK, w32.MC_CHECKMARKNORMAL, m.checkWidth, m.checkHeight)
	if checkInk <= 0 {
		checkInk = contentBox(m.checkHeight, m.checkMarginY)
	}
	m.checkFont, m.checkChar = newInkGlyph(hwnd, checkInk, iconCheck, marlettCheck)

	// The bullet occupies less of the check part and needs its own measurement.
	bulletInk := themedInkHeight(hwnd, w32.MENU_POPUPCHECK, w32.MC_BULLETNORMAL, m.checkWidth, m.checkHeight)
	if bulletInk <= 0 {
		bulletInk = checkInk
	}
	m.bulletFont, m.bulletChar = newInkGlyph(hwnd, bulletInk, iconBullet, marlettBullet)
	arrowInk := themedInkHeight(hwnd, w32.MENU_POPUPSUBMENU, w32.MSM_NORMAL, m.submenuGlyphW, m.submenuGlyph)
	if arrowInk <= 0 {
		arrowInk = contentBox(m.submenuGlyph, m.submenuMarginY)
	}
	m.submenuFont, m.submenuChar = newInkGlyph(hwnd, arrowInk, iconChevronRight, marlettSubmenu)

	return m
}

// applyThemeMetrics reads raw geometry; finalise combines it with font metrics.
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
	// The check part, not its background, owns the glyph padding.
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

	// Windows reserves the submenu column on every item.
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

	// Theme margins remain in 96-DPI artwork units. Native menus scale their
	// vertical contribution and label reserve, but not horizontal padding.
	scale := themeScale(hwnd, hdc, m.checkHeight)
	m.checkMarginY = scale.apply(m.checkMarginY)
	m.itemPadY = scale.apply(m.itemPadY)
	m.labelReserveScaled = scale.apply(labelReserve)
}

// themeRatio scales 96-DPI theme margins using exact integer arithmetic.
type themeRatio struct{ have, base int }

// apply leaves v unchanged when the theme measurement is unavailable.
func (r themeRatio) apply(v int) int {
	if r.base <= 0 || r.have <= 0 {
		return v
	}
	return v * r.have / r.base
}

// themeScale compares the current check artwork with the same part at 96 DPI.
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

func (w *windowsWebviewWindow) invalidateMenuMetrics() {
	w.menuMetrics.release()
	w.menuMetrics = nil
}

// menuItemFor resolves the stable native data sent with owner-draw messages.
func (w *windowsWebviewWindow) menuItemFor(itemData uintptr) (*MenuItem, bool) {
	if w.menu == nil || w.menu.drawMapping == nil {
		return nil, false
	}
	entry, ok := w.menu.drawMapping[itemData]
	if !ok || entry == nil || entry.item == nil {
		return nil, false
	}
	if err := entry.data.setText(menuItemDisplayText(entry.item)); err != nil {
		if globalApplication != nil {
			globalApplication.error("unable to refresh accessible menu label: %v", err)
		}
	}
	return entry.item, true
}

func menuItemDisplayText(item *MenuItem) string {
	text := item.label
	if item.accelerator != nil {
		text += "\t" + item.accelerator.String()
	}
	return text
}

func menuItemLabelAccel(item *MenuItem) (label string, accel string) {
	label = item.label
	if item.accelerator != nil {
		accel = item.accelerator.String()
	}
	return label, accel
}

func menuMnemonic(label string) (rune, bool) {
	runes := []rune(label)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '&' || i+1 >= len(runes) {
			continue
		}
		if runes[i+1] == '&' {
			i++
			continue
		}
		return unicode.ToUpper(runes[i+1]), true
	}
	return 0, false
}

func menuCharResult(index int, action uint16) uintptr {
	return uintptr(uint16(index)) | uintptr(action)<<16
}

// handleMenuChar restores mnemonic navigation, which Windows cannot infer for
// owner-drawn strings.
func (w *windowsWebviewWindow) handleMenuChar(wparam, lparam uintptr) (uintptr, bool) {
	hmenu := w32.HMENU(lparam)
	count := w32.GetMenuItemCount(hmenu)
	if count <= 0 {
		return 0, false
	}

	key := unicode.ToUpper(rune(uint16(wparam)))
	selected := -1
	owned := false
	matches := make([]int, 0, 1)
	for i := 0; i < count; i++ {
		mii := w32.MENUITEMINFO{
			CbSize: uint32(unsafe.Sizeof(w32.MENUITEMINFO{})),
			FMask:  w32.MIIM_DATA | w32.MIIM_STATE,
		}
		if !w32.GetMenuItemInfo(hmenu, uint32(i), true, &mii) {
			continue
		}
		item, ok := w.menuItemFor(mii.DwItemData)
		if !ok {
			continue
		}
		owned = true
		if mii.FState&w32.MFS_HILITE != 0 {
			selected = i
		}
		mnemonic, ok := menuMnemonic(item.label)
		if ok && mnemonic == key && mii.FState&(w32.MFS_DISABLED|w32.MFS_GRAYED) == 0 {
			matches = append(matches, i)
		}
	}

	if !owned {
		return 0, false
	}
	if len(matches) == 0 {
		return menuCharResult(0, w32.MNC_IGNORE), true
	}
	if len(matches) == 1 {
		return menuCharResult(matches[0], w32.MNC_EXECUTE), true
	}
	for _, index := range matches {
		if index > selected {
			return menuCharResult(index, w32.MNC_SELECT), true
		}
	}
	return menuCharResult(matches[0], w32.MNC_SELECT), true
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

// widestAccelerator sizes the shared accelerator column for one popup. hdc must
// already have the menu font selected.
func widestAccelerator(hdc w32.HDC, item *MenuItem) int {
	impl, ok := item.impl.(*windowsMenuItem)
	if !ok || impl.parent == nil {
		return 0
	}

	widest := 0
	for _, sibling := range impl.parent.items {
		if sibling.accelerator == nil || sibling.Hidden() {
			continue
		}
		if w, _ := measureText(hdc, sibling.accelerator.String()); w > widest {
			widest = w
		}
	}
	return widest
}

// measureText returns the extent of s in the font selected into hdc.
//
// GetTextExtentPoint32 counts UTF-16 code units, not runes. Passing a rune
// count measures a character short for every surrogate pair, so a label with an
// emoji in it sizes its popup too narrow and gets clipped.
func measureText(hdc w32.HDC, s string) (int, int) {
	if s == "" {
		return 0, 0
	}
	chars := w32.MustStringToUTF16(s)
	chars = chars[:len(chars)-1] // drop the terminating NUL
	var sz w32.SIZE
	if !w32.GetTextExtentPoint32(hdc, &chars[0], len(chars), &sz) {
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

	var colours menuColours
	if w.menuOwnerDrawDark {
		colours = darkMenuColours
	} else if m := w.currentMenuMetrics(); m != nil && m.havePalette {
		// Light mode has a native menu to sample. Win32 exposes no equivalent
		// dark popup palette, so dark mode uses the built-in colours above.
		colours = m.lightPalette
	} else {
		colours = lightMenuColours
	}

	return applyCustomPopupColours(colours, w.customMenuBarTheme())
}

// customMenuBarTheme returns the resolved internal theme only when the user
// supplied the matching public light/dark MenuBarTheme. The built-in dark menu
// bar should not replace the standard popup palette.
func (w *windowsWebviewWindow) customMenuBarTheme() *w32.MenuBarTheme {
	if w == nil || w.parent == nil || w.menubarTheme == nil {
		return nil
	}
	custom := w.parent.options.Windows.CustomTheme
	if w.menuOwnerDrawDark {
		if custom.DarkModeMenuBar == nil {
			return nil
		}
	} else if custom.LightModeMenuBar == nil {
		return nil
	}
	return w.menubarTheme
}

// applyCustomPopupColours preserves the existing MenuBarTheme meaning:
// Default paints normal rows and Selected paints the active popup row. Popup-
// only supporting colours are derived because the public theme has no fields
// for them.
func applyCustomPopupColours(colours menuColours, theme *w32.MenuBarTheme) menuColours {
	if theme == nil {
		return colours
	}
	if theme.MenuBarBackground != nil {
		colours.background = *theme.MenuBarBackground
	}
	if theme.TitleBarText != nil {
		colours.text = *theme.TitleBarText
	}
	if theme.MenuSelectedBackground != nil {
		colours.selectedBg = *theme.MenuSelectedBackground
	}
	if theme.MenuSelectedText != nil {
		colours.selectedText = *theme.MenuSelectedText
	}

	colours.checkBackground = colours.selectedBg
	colours.disabledText = mix(colours.background, colours.text, 50)
	colours.separator = mix(colours.background, colours.text, 15)
	return colours
}

// drawCheck paints the checkmark and the background panel behind it, centred in
// the gutter at the size the theme reports.
//
// Windows draws MENU_POPUPCHECK inside MENU_POPUPCHECKBACKGROUND, and the panel
// is what makes a checked item read as checked at a glance. DrawThemeBackground
// is not used for either: it would paint them in the system theme's colours,
// which on a light system means a dark glyph on our dark background.
func (m *menuMetrics) drawCheck(hdc w32.HDC, rect w32.RECT, colours menuColours, selected, disabled, radio bool) {
	// The panel is the background of the check column, not a box around the
	// glyph: it spans the item's full height, so two checked rows next to each
	// other form one continuous strip the way a native menu does. Drawing it at
	// the checkmark's own height instead leaves a gap between them.
	//
	// Its width is the check plus the margins either side, which is the gutter
	// less the gap that separates the column from the label.
	panel := w32.RECT{
		Left:   rect.Left + int32(m.itemPadLeft),
		Right:  rect.Left + int32(m.itemPadLeft+m.checkWidth+m.checkMarginX),
		Top:    rect.Top,
		Bottom: rect.Bottom,
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
	item, ok := w.menuItemFor(mis.ItemData)
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
		accelW = widestAccelerator(hdc, item)
	})
	w32.ReleaseDC(w.hwnd, hdc)

	// Native popups reserve label and submenu columns on every row.
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
	item, ok := w.menuItemFor(dis.ItemData)
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

	surface := w32.CreateSolidBrush(colours.background)
	w32.FillRect(dis.HDC, &rect, surface)
	w32.DeleteObject(w32.HGDIOBJ(surface))

	// Theme padding captures the square Windows 10 and inset Windows 11 bands.
	if selected && !disabled && !isSeparator {
		band := rect
		band.Left += int32(metrics.itemPadLeft)
		band.Right -= int32(metrics.itemPadRight)
		hot := w32.CreateSolidBrush(colours.selectedBg)

		radius := metrics.itemPadLeft
		if radius > 0 {
			rgn := w32.CreateRoundRectRgn(int(band.Left), int(band.Top),
				int(band.Right), int(band.Bottom), radius*2, radius*2)
			if rgn != 0 {
				w32.FillRgn(dis.HDC, rgn, hot)
				w32.DeleteObject(w32.HGDIOBJ(rgn))
			} else {
				w32.FillRect(dis.HDC, &band, hot)
			}
		} else {
			w32.FillRect(dis.HDC, &band, hot)
		}
		w32.DeleteObject(w32.HGDIOBJ(hot))
	}

	if isSeparator {
		lineBrush := w32.CreateSolidBrush(colours.separator)
		mid := rect.Top + (rect.Bottom-rect.Top)/2
		// Native separators span the text column, not the check gutter.
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

	// USER32 draws submenu arrows after WM_DRAWITEM. Leave its source text colour
	// in place; the popup subclass replaces the classic triangle afterward.
	if !hasSubmenu && accel != "" {
		accelRect := labelRect
		drawMenuString(dis.HDC, accel, &accelRect, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	// Submenu arrows consume this colour after the callback returns.
	if !hasSubmenu {
		w32.SetTextColor(dis.HDC, oldTextColour)
	}
	w32.SetBkMode(dis.HDC, oldBkMode)
	if oldFont != 0 {
		w32.SelectObject(dis.HDC, oldFont)
	}
	return true
}
