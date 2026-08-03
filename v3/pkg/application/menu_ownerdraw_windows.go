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

type menuColours struct {
	background   uint32
	text         uint32
	disabledText uint32
	selectedBg   uint32
	selectedText uint32
	separator    uint32
}

// rgb packs to COLORREF byte order (0x00BBGGRR).
func rgb(r, g, b uint32) uint32 { return r | g<<8 | b<<16 }

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

const (
	menuItemPaddingX  = 12 // left/right padding inside a popup item
	menuCheckGutter   = 22 // reserved width for the checkmark column
	menuAccelGap      = 28 // gap between label and accelerator
	menuSeparatorSize = 7  // total height of a separator item
	menuItemMinHeight = 22
)

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

// menuFont builds the Segoe UI menu font, matching w32/menubar.go.
//
// w32.GetStockObject cannot be used here: it calls procGetDeviceCaps rather than
// procGetStockObject, so it always returns 0. Passing that 0 to w32.SelectObject,
// which panics on failure instead of returning an error, kills the process from
// inside the Windows callback.
func menuFont() w32.HFONT {
	lf := w32.LOGFONT{
		Height:         -12,
		Weight:         400,
		CharSet:        1,
		Quality:        5,
		PitchAndFamily: 0,
	}
	name := []uint16{'S', 'e', 'g', 'o', 'e', ' ', 'U', 'I', 0}
	copy(lf.FaceName[:], name)
	return w32.CreateFontIndirect(&lf)
}

// withMenuFont selects the menu font for the duration of fn. It never hands
// SelectObject a 0 handle.
func withMenuFont(hdc w32.HDC, fn func()) {
	hFont := menuFont()
	if hFont == 0 {
		fn()
		return
	}
	old := w32.SelectObject(hdc, w32.HGDIOBJ(hFont))
	fn()
	if old != 0 {
		w32.SelectObject(hdc, old)
	}
	w32.DeleteObject(w32.HGDIOBJ(hFont))
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

	item, ok := w.menuItemFor(mis.ItemID)
	if !ok {
		mis.ItemHeight = menuItemMinHeight
		mis.ItemWidth = 100
		return true
	}
	if item.IsSeparator() {
		mis.ItemHeight = menuSeparatorSize
		mis.ItemWidth = 0
		return true
	}

	label, accel := menuItemLabelAccel(item)

	hdc := w32.GetDC(w.hwnd)
	if hdc == 0 {
		mis.ItemHeight = menuItemMinHeight
		mis.ItemWidth = 100
		return true
	}
	var labelW, labelH, accelW int
	withMenuFont(hdc, func() {
		labelW, labelH = measureText(hdc, label)
		accelW, _ = measureText(hdc, accel)
	})
	w32.ReleaseDC(w.hwnd, hdc)

	width := menuCheckGutter + labelW + menuItemPaddingX
	if accelW > 0 {
		width += menuAccelGap + accelW
	}
	height := labelH + 6
	if height < menuItemMinHeight {
		height = menuItemMinHeight
	}

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
			Left:   rect.Left + menuItemPaddingX,
			Top:    mid,
			Right:  rect.Right - menuItemPaddingX,
			Bottom: mid + 1,
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

	hFont := menuFont()
	var oldFont w32.HGDIOBJ
	if hFont != 0 {
		oldFont = w32.SelectObject(dis.HDC, w32.HGDIOBJ(hFont))
	}
	w32.SetBkMode(dis.HDC, w32.TRANSPARENT)
	w32.SetTextColor(dis.HDC, w32.COLORREF(textColour))

	if dis.ItemState&w32.ODS_CHECKED != 0 {
		check := rect
		check.Left += 4
		check.Right = check.Left + menuCheckGutter
		drawMenuString(dis.HDC, "✓", &check, w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	labelRect := rect
	labelRect.Left += menuCheckGutter
	labelRect.Right -= menuItemPaddingX
	drawMenuString(dis.HDC, label, &labelRect, w32.DT_LEFT|w32.DT_SINGLELINE|w32.DT_VCENTER)

	if hasSubmenu {
		arrow := rect
		arrow.Left = labelRect.Left
		arrow.Right -= 6
		drawMenuString(dis.HDC, "▸", &arrow, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	} else if accel != "" {
		accelRect := rect
		accelRect.Left = labelRect.Left
		accelRect.Right -= menuItemPaddingX
		w32.SetTextColor(dis.HDC, w32.COLORREF(colours.disabledText))
		drawMenuString(dis.HDC, accel, &accelRect, w32.DT_RIGHT|w32.DT_SINGLELINE|w32.DT_VCENTER)
	}

	if oldFont != 0 {
		w32.SelectObject(dis.HDC, oldFont)
	}
	if hFont != 0 {
		w32.DeleteObject(w32.HGDIOBJ(hFont))
	}
	return true
}
