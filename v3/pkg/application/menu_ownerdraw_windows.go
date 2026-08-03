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
	disabledText    uint32
	selectedBg      uint32
	selectedText    uint32
	separator       uint32
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
	// the arrow never overlaps a long label.
	submenuWidth int
	accelGap     int
	minHeight    int
	sepHeight    int

	// themed records whether the measurements came from the visual style.
	themed bool
}

// accelGapChars is the space between a label and its accelerator, in average
// character widths of the menu font. The theme has no property for it.
const accelGapChars = 4

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
	}
	if mg, ok := w32.GetThemeMargins(hTheme, hdc, w32.MENU_POPUPSUBMENU, w32.MSM_NORMAL, w32.TMT_CONTENTMARGINS); ok {
		m.submenuWidth += int(mg.CxLeftWidth + mg.CxRightWidth)
	}

	if sz, ok := w32.GetThemePartSize(hTheme, hdc, w32.MENU_POPUPSEPARATOR, 0, w32.TS_TRUE); ok {
		m.sepRule = int(sz.CY)
	}
}

// applyFontMetrics records what depends on the menu font. hdc must already have
// that font selected.
func (m *menuMetrics) applyFontMetrics(hdc w32.HDC) {
	var tm w32.TEXTMETRIC
	if !w32.GetTextMetrics(hdc, &tm) {
		return
	}
	m.textHeight = int(tm.TmHeight + tm.TmExternalLeading)
	m.accelGap = int(tm.TmAveCharWidth) * accelGapChars
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
	if m.accelGap <= 0 {
		m.accelGap = edgeX * accelGapChars
	}
	if m.textHeight <= 0 {
		m.textHeight = w32.SystemMetricForDpi(w32.SM_CYMENU, m.dpi)
	}
	if m.sepRule <= 0 {
		m.sepRule = 2*w32.SystemMetricForDpi(w32.SM_CYBORDER, m.dpi) + 1
	}
}

// textLeft is where an item's label starts.
func (m *menuMetrics) textLeft() int { return m.gutterWidth + m.itemPadLeft }

func (m *menuMetrics) release() {
	if m == nil {
		return
	}
	if m.font != 0 {
		w32.DeleteObject(w32.HGDIOBJ(m.font))
		m.font = 0
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
func (w *windowsWebviewWindow) menuItemFor(itemID uint32) (*MenuItem, bool) {
	if w.menu == nil || w.menu.drawMapping == nil {
		return nil, false
	}
	item, ok := w.menu.drawMapping[int(itemID)]
	return item, ok && item != nil
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
		if sibling.accelerator == nil {
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

func (w *windowsWebviewWindow) menuColours() menuColours {
	if w.menuOwnerDrawDark {
		return darkMenuColours
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

	panel := w32.RECT{
		Left:   rect.Left + int32(m.itemPadLeft),
		Right:  rect.Left + int32(m.itemPadLeft+m.checkWidth),
		Top:    rect.Top + int32(max((height-m.checkHeight)/2, 0)),
		Bottom: rect.Top + int32(max((height-m.checkHeight)/2, 0)+m.checkHeight),
	}

	if !selected {
		panelBrush := w32.CreateSolidBrush(colours.checkBackground)
		w32.FillRect(hdc, &panel, panelBrush)
		w32.DeleteObject(w32.HGDIOBJ(panelBrush))
	}

	glyph := "✓"
	if radio {
		glyph = "●"
	}

	previous := w32.SetTextColor(hdc, w32.COLORREF(colours.text))
	if disabled {
		w32.SetTextColor(hdc, w32.COLORREF(colours.disabledText))
	} else if selected {
		w32.SetTextColor(hdc, w32.COLORREF(colours.selectedText))
	}
	drawMenuString(hdc, glyph, &panel, w32.DT_CENTER|w32.DT_SINGLELINE|w32.DT_VCENTER)
	w32.SetTextColor(hdc, previous)
}

// handleMeasureMenuItem sizes an owner-drawn popup item. Without this, Windows
// has no size for the item and renders it collapsed.
func (w *windowsWebviewWindow) handleMeasureMenuItem(lparam uintptr) bool {
	mis := (*w32.MEASUREITEMSTRUCT)(unsafe.Pointer(lparam))
	if mis.CtlType != w32.ODT_MENU {
		return false
	}

	metrics := w.currentMenuMetrics()

	item, ok := w.menuItemFor(mis.ItemID)
	if !ok {
		mis.ItemHeight = uint32(metrics.minHeight)
		mis.ItemWidth = uint32(metrics.textLeft())
		return true
	}
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
	width := metrics.textLeft() + labelW + metrics.itemPadRight + metrics.submenuWidth
	if accelW > 0 {
		width += metrics.accelGap + accelW
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

	metrics := w.currentMenuMetrics()
	colours := w.menuColours()
	rect := dis.RcItem

	item, ok := w.menuItemFor(dis.ItemID)
	var label, accel string
	var hasSubmenu, isSeparator bool
	if ok {
		label, accel = menuItemLabelAccel(item)
		hasSubmenu = item.submenu != nil
		isSeparator = item.IsSeparator()
	}

	selected := dis.ItemState&w32.ODS_SELECTED != 0
	disabled := dis.ItemState&(w32.ODS_GRAYED|w32.ODS_DISABLED) != 0

	bg := colours.background
	if selected && !disabled && !isSeparator {
		bg = colours.selectedBg
	}
	bgBrush := w32.CreateSolidBrush(bg)
	w32.FillRect(dis.HDC, &rect, bgBrush)
	w32.DeleteObject(w32.HGDIOBJ(bgBrush))

	// Only a genuine separator draws a rule. An item that failed to resolve gets
	// the background and nothing else - drawing a rule there would disguise a
	// lookup failure as a deliberate separator, which is how the submenu items
	// silently rendered as blank rules.
	if !ok {
		return true
	}

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

	var oldFont w32.HGDIOBJ
	if metrics.font != 0 {
		oldFont = w32.SelectObject(dis.HDC, w32.HGDIOBJ(metrics.font))
	}
	w32.SetBkMode(dis.HDC, w32.TRANSPARENT)
	w32.SetTextColor(dis.HDC, w32.COLORREF(textColour))

	if dis.ItemState&w32.ODS_CHECKED != 0 {
		metrics.drawCheck(dis.HDC, rect, colours, selected, disabled, item.IsRadio())
	}

	// The label occupies the text column: right of the gutter, left of the
	// reserved submenu column.
	labelRect := rect
	labelRect.Left += int32(metrics.textLeft())
	labelRect.Right -= int32(metrics.itemPadRight + metrics.submenuWidth)
	drawMenuString(dis.HDC, label, &labelRect, w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)

	if hasSubmenu {
		// Drawn inside the column reserved for it in handleMeasureMenuItem, so
		// it cannot land on top of a long label.
		arrow := rect
		arrow.Left = rect.Right - int32(metrics.itemPadRight+metrics.submenuWidth)
		arrow.Right = rect.Right - int32(metrics.itemPadRight)
		drawMenuString(dis.HDC, "▸", &arrow, w32.DT_CENTER|w32.DT_SINGLELINE|w32.DT_VCENTER)
	} else if accel != "" {
		accelRect := labelRect
		w32.SetTextColor(dis.HDC, w32.COLORREF(colours.disabledText))
		drawMenuString(dis.HDC, accel, &accelRect, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	if oldFont != 0 {
		w32.SelectObject(dis.HDC, oldFont)
	}
	return true
}
