//go:build windows && !server

package application

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

// Replacing the submenu arrow Windows draws for owner-drawn items.
//
// For an owner-drawn item that opens a submenu, USER32 draws the arrow itself
// once WM_DRAWITEM has returned, and it uses the classic triangle rather than
// the theme's chevron because owner-draw bypasses the themed path. Anything
// drawn in that column from WM_DRAWITEM is therefore painted over a moment
// later - confirmed by deleting that drawing entirely and watching the triangle
// still appear.
//
// The only place left to draw is the popup window itself, after Windows has
// finished with it. WM_ENTERIDLE is sent to the menu's owner whenever the menu
// loop runs out of messages, with the popup's handle in lParam, which is both a
// handle to draw on and a signal that painting has settled. Hovering an item
// repaints it and briefly restores the triangle; the next idle puts the chevron
// back.

// MSGF_MENU identifies a WM_ENTERIDLE that came from a menu rather than a
// dialog.
const MSGF_MENU = 2

// MN_SELECTITEM is sent to a popup's window when the highlighted item changes.
// It is the message that repaints an item on hover, and therefore the one that
// puts the classic triangle back.
const MN_SELECTITEM = 0x01E5

// Popups have to be subclassed rather than repainted on idle. WM_ENTERIDLE only
// arrives once the menu loop runs out of messages, so moving the pointer across
// an item repaints it - triangle and all - and the chevron does not return until
// the pointer stops. That reads as a flicker back to the wrong glyph.
//
// Subclassing lets the arrows be redrawn immediately after each repaint. The
// original procedure is kept per window and restored when the menu loop ends,
// because USER32 pools and reuses these windows.
var (
	subclassed    sync.Map // w32.HWND -> uintptr, the original window procedure
	subclassProc  = syscall.NewCallback(popupWndProc)
	subclassOwner sync.Map // w32.HWND -> *windowsWebviewWindow
)

// subclassPopup takes over popup's window procedure, once.
func (w *windowsWebviewWindow) subclassPopup(popup w32.HWND) {
	if popup == 0 {
		return
	}
	if _, already := subclassed.Load(popup); already {
		return
	}
	previous := w32.SetWindowLongPtr(popup, w32.GWLP_WNDPROC, subclassProc)
	if previous == 0 {
		return
	}
	subclassed.Store(popup, previous)
	subclassOwner.Store(popup, w)
}

// releasePopups restores every popup procedure this window replaced. Menu
// windows are reused by USER32, so leaving a subclass on one would have it
// still installed the next time some other menu borrows that window.
func releasePopups() {
	subclassed.Range(func(key, value any) bool {
		popup := key.(w32.HWND)
		w32.SetWindowLongPtr(popup, w32.GWLP_WNDPROC, value.(uintptr))
		subclassed.Delete(popup)
		subclassOwner.Delete(popup)
		return true
	})
}

func popupWndProc(popup w32.HWND, msg uint32, wparam, lparam uintptr) uintptr {
	original, ok := subclassed.Load(popup)
	if !ok {
		return w32.DefWindowProc(popup, msg, wparam, lparam)
	}

	result := w32.CallWindowProc(original.(uintptr), popup, msg, wparam, lparam)

	// After the default handling, not before: these are the messages that leave
	// a freshly drawn triangle behind.
	switch msg {
	case w32.WM_PAINT, MN_SELECTITEM, w32.WM_PRINTCLIENT:
		if owner, ok := subclassOwner.Load(popup); ok {
			owner.(*windowsWebviewWindow).paintSubmenuArrows(popup)
		}
	}
	return result
}

// paintSubmenuArrows covers the arrows Windows drew on popup with the chevron a
// native menu would have used.
//
// Silently does nothing when the metrics have no icon font, which is the case
// under the classic style and in High Contrast - there the classic triangle is
// the correct shape anyway.
func (w *windowsWebviewWindow) paintSubmenuArrows(popup w32.HWND) {
	if popup == 0 || w.menu == nil {
		return
	}
	metrics := w.currentMenuMetrics()
	if metrics == nil || metrics.submenuFont == 0 || metrics.submenuChar == "" {
		return
	}

	hmenu := w32.HMENU(w32.SendMessage(popup, w32.MN_GETHMENU, 0, 0))
	if hmenu == 0 {
		return
	}
	count := w32.GetMenuItemCount(hmenu)
	if count <= 0 {
		return
	}

	colours := w.menuColours()

	hdc := w32.GetDC(popup)
	if hdc == 0 {
		return
	}
	defer w32.ReleaseDC(popup, hdc)

	oldBkMode := w32.SetBkMode(hdc, w32.TRANSPARENT)
	defer w32.SetBkMode(hdc, oldBkMode)

	for i := range count {
		var mii w32.MENUITEMINFO
		mii.CbSize = uint32(unsafe.Sizeof(mii))
		mii.FMask = w32.MIIM_SUBMENU | w32.MIIM_STATE | w32.MIIM_FTYPE
		if !w32.GetMenuItemInfo(hmenu, uint32(i), true, &mii) {
			continue
		}
		// Only items this window owner-draws and that actually open a submenu
		// have an arrow to replace.
		if mii.HSubMenu == 0 || mii.FType&w32.MFT_OWNERDRAW == 0 {
			continue
		}

		var screen w32.RECT
		if !w32.GetMenuItemRect(w.hwnd, hmenu, uint32(i), &screen) {
			continue
		}
		rect, ok := toClient(popup, screen)
		if !ok || rect.Bottom-rect.Top <= 0 {
			continue
		}

		selected := mii.FState&w32.MFS_HILITE != 0
		disabled := mii.FState&(w32.MFS_DISABLED|w32.MFS_GRAYED) != 0

		bg := colours.background
		ink := colours.text
		switch {
		case disabled:
			ink = colours.disabledText
		case selected:
			bg, ink = colours.selectedBg, colours.selectedText
		}

		arrow := w32.RECT{
			Left:   rect.Right - int32(metrics.itemPadRight+metrics.submenuWidth),
			Right:  rect.Right - int32(metrics.itemPadRight),
			Top:    rect.Top,
			Bottom: rect.Bottom,
		}

		// Cover the triangle before drawing over it, or the two shapes overlap.
		cover := w32.CreateSolidBrush(bg)
		w32.FillRect(hdc, &arrow, cover)
		w32.DeleteObject(w32.HGDIOBJ(cover))

		previous := w32.SetTextColor(hdc, w32.COLORREF(ink))
		drawSymbol(hdc, metrics.submenuFont, metrics.submenuChar, arrow)
		w32.SetTextColor(hdc, previous)
	}
}

// toClient converts a screen rect to hwnd's client coordinates.
func toClient(hwnd w32.HWND, r w32.RECT) (w32.RECT, bool) {
	left, top, ok := w32.ScreenToClient(hwnd, int(r.Left), int(r.Top))
	if !ok {
		return r, false
	}
	right, bottom, ok := w32.ScreenToClient(hwnd, int(r.Right), int(r.Bottom))
	if !ok {
		return r, false
	}
	return w32.RECT{
		Left:   int32(left),
		Top:    int32(top),
		Right:  int32(right),
		Bottom: int32(bottom),
	}, true
}
