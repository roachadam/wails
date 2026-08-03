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
	background   uint32
	text         uint32
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
	background:   rgb(33, 33, 33),
	text:         rgb(222, 222, 222),
	disabledText: rgb(110, 110, 110),
	selectedBg:   rgb(62, 62, 64),
	selectedText: rgb(255, 255, 255),
	separator:    rgb(60, 60, 60),
}

var lightMenuColours = menuColours{
	background:   rgb(255, 255, 255),
	text:         rgb(0, 0, 0),
	disabledText: rgb(109, 109, 109),
	selectedBg:   rgb(204, 232, 255),
	selectedText: rgb(0, 0, 0),
	separator:    rgb(215, 215, 215),
}

// menuMetrics holds the layout for one DPI. Nothing here is a fixed pixel
// count: the values come from the user's menu font and the system metrics for
// the window's DPI, so items follow the display scaling and any font size the
// user has chosen in Windows.
//
// Sources:
//
//   - font comes from NONCLIENTMETRICS.MenuFont, which is the font Windows
//     itself uses for menus, already scaled for the requested DPI.
//   - checkGutter comes from SM_CXMENUCHECK, the width Windows reserves for a
//     menu checkmark.
//   - minHeight has SM_CYMENUCHECK as its floor so a checkmark always fits, and
//     otherwise follows the text height.
//   - paddingX, accelGap and separatorHeight have no system metric of their own.
//     They are expressed as multiples of the font's average character width and
//     line height, so they scale with the font instead of being fixed pixels.
type menuMetrics struct {
	dpi w32.UINT

	font        w32.HFONT
	checkGutter int
	paddingX    int
	accelGap    int
	minHeight   int
	sepHeight   int
}

// accelGapChars is the space between a label and its accelerator, in average
// character widths of the menu font.
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

	m := &menuMetrics{
		dpi:         dpi,
		font:        menuFontForDpi(dpi),
		checkGutter: w32.SystemMetricForDpi(w32.SM_CXMENUCHECK, dpi),
		minHeight:   w32.SystemMetricForDpi(w32.SM_CYMENUCHECK, dpi),
	}

	// The remaining values depend on the font, so they need a device context
	// with that font selected.
	hdc := w32.GetDC(hwnd)
	if hdc != 0 {
		var old w32.HGDIOBJ
		if m.font != 0 {
			old = w32.SelectObject(hdc, w32.HGDIOBJ(m.font))
		}

		var tm w32.TEXTMETRIC
		if w32.GetTextMetrics(hdc, &tm) {
			m.paddingX = int(tm.TmAveCharWidth)
			m.accelGap = int(tm.TmAveCharWidth) * accelGapChars

			lineHeight := int(tm.TmHeight + tm.TmExternalLeading)
			// One edge above and below the text, matching the spacing Windows
			// uses between a menu item's text and its bounds.
			m.minHeight = max(m.minHeight, lineHeight+2*w32.SystemMetricForDpi(w32.SM_CYEDGE, dpi))
			// A separator is a rule with space around it rather than a text
			// row, so it takes half a line.
			m.sepHeight = lineHeight / 2
		}

		if old != 0 {
			w32.SelectObject(hdc, old)
		}
		w32.ReleaseDC(hwnd, hdc)
	}

	// Floors for the cases where the device context or text metrics were
	// unavailable. Border width is itself a DPI-scaled system metric, so these
	// still scale.
	border := w32.SystemMetricForDpi(w32.SM_CYBORDER, dpi)
	if m.paddingX <= 0 {
		m.paddingX = w32.SystemMetricForDpi(w32.SM_CXEDGE, dpi)
	}
	if m.accelGap <= 0 {
		m.accelGap = m.paddingX * accelGapChars
	}
	if m.minHeight <= 0 {
		m.minHeight = w32.SystemMetricForDpi(w32.SM_CYMENU, dpi)
	}
	m.sepHeight = max(m.sepHeight, 2*border+1)

	// The gutter has to hold the checkmark and still leave the label clear of
	// it.
	m.checkGutter += m.paddingX

	return m
}

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

// menuItemFor resolves an owner-drawn item from the menu's own mapping, keyed by
// the Win32 item id.
//
// The global menuItemMap is not usable here: Menu.AddSeparator never registers
// its item, so getMenuItemByID returns nil for every separator. Win32Menu's
// menuMapping is populated for all items during buildMenu, separators included,
// which lets separators be identified explicitly rather than inferred from a
// failed lookup.
func (w *windowsWebviewWindow) menuItemFor(itemID uint32) (*MenuItem, bool) {
	if w.menu == nil || w.menu.menuMapping == nil {
		return nil, false
	}
	item, ok := w.menu.menuMapping[int(itemID)]
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
		mis.ItemWidth = uint32(metrics.checkGutter)
		return true
	}
	if item.IsSeparator() {
		mis.ItemHeight = uint32(metrics.sepHeight)
		mis.ItemWidth = 0
		return true
	}

	label, accel := menuItemLabelAccel(item)

	hdc := w32.GetDC(w.hwnd)
	if hdc == 0 {
		mis.ItemHeight = uint32(metrics.minHeight)
		mis.ItemWidth = uint32(metrics.checkGutter)
		return true
	}
	var labelW, labelH, accelW int
	metrics.withMenuFont(hdc, func() {
		labelW, labelH = measureText(hdc, label)
		accelW, _ = measureText(hdc, accel)
	})
	w32.ReleaseDC(w.hwnd, hdc)

	width := metrics.checkGutter + labelW + metrics.paddingX
	if accelW > 0 {
		width += metrics.accelGap + accelW
	}
	height := max(labelH+2*w32.SystemMetricForDpi(w32.SM_CYEDGE, metrics.dpi), metrics.minHeight)

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

	if !ok || isSeparator {
		lineBrush := w32.CreateSolidBrush(colours.separator)
		mid := rect.Top + (rect.Bottom-rect.Top)/2
		line := w32.RECT{
			Left:   rect.Left + int32(metrics.paddingX),
			Top:    mid,
			Right:  rect.Right - int32(metrics.paddingX),
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
		check := rect
		check.Left += int32(w32.SystemMetricForDpi(w32.SM_CXEDGE, metrics.dpi))
		check.Right = check.Left + int32(metrics.checkGutter)
		drawMenuString(dis.HDC, "✓", &check, w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	labelRect := rect
	labelRect.Left += int32(metrics.checkGutter)
	labelRect.Right -= int32(metrics.paddingX)
	drawMenuString(dis.HDC, label, &labelRect, w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)

	if hasSubmenu {
		arrow := rect
		arrow.Left = labelRect.Left
		arrow.Right -= int32(w32.SystemMetricForDpi(w32.SM_CXEDGE, metrics.dpi))
		drawMenuString(dis.HDC, "▸", &arrow, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	} else if accel != "" {
		accelRect := rect
		accelRect.Left = labelRect.Left
		accelRect.Right -= int32(metrics.paddingX)
		w32.SetTextColor(dis.HDC, w32.COLORREF(colours.disabledText))
		drawMenuString(dis.HDC, accel, &accelRect, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	if oldFont != 0 {
		w32.SelectObject(dis.HDC, oldFont)
	}
	return true
}
