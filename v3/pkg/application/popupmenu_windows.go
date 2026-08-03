//go:build windows && !server

package application

import (
	"fmt"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

const (
	MenuItemMsgID = w32.WM_APP + 1024
)

type RadioGroupMember struct {
	ID       int
	MenuItem *MenuItem
}

type RadioGroup []*RadioGroupMember

func (r *RadioGroup) Add(id int, item *MenuItem) {
	*r = append(*r, &RadioGroupMember{
		ID:       id,
		MenuItem: item,
	})
}

func (r *RadioGroup) Bounds() (int, int) {
	p := *r
	return p[0].ID, p[len(p)-1].ID
}

func (r *RadioGroup) MenuID(item *MenuItem) int {
	for _, member := range *r {
		if member.MenuItem == item {
			return member.ID
		}
	}
	panic("RadioGroup.MenuID: item not found:")
}

type Win32Menu struct {
	isPopup      bool
	menu         w32.HMENU
	parentWindow *windowsWebviewWindow
	parent       w32.HWND
	menuMapping  map[int]*MenuItem
	// drawMapping resolves items for owner-draw, keyed by the identifier
	// AppendMenu was actually given. That is the sequential command id for a
	// normal item, but the submenu's HMENU for an MF_POPUP item, and WM_DRAWITEM
	// reports whichever was used. menuMapping cannot serve both because it is
	// keyed by command id for dispatch.
	drawMapping   map[int]*MenuItem
	checkboxItems map[*MenuItem][]int
	radioGroups   map[*MenuItem][]*RadioGroup
	menuData      *Menu
	currentMenuID int
	onMenuClose   func()
	onMenuOpen    func()
	isShowing     atomic.Bool // guards against concurrent TrackPopupMenuEx calls

	// bitmaps tracks HBITMAP handles allocated by SetMenuIcons during
	// buildMenu so they can be released when the menu is rebuilt or
	// destroyed. DestroyMenu does not free bitmaps set via
	// SetMenuItemBitmaps.
	bitmaps []w32.HBITMAP
}

// releaseMenuBitmaps frees every HBITMAP in bitmaps and every runtime-
// SetBitmap handle reachable via mapping, clearing impl.bitmap to 0 as it
// goes. Ownership invariant: the two collections must be disjoint — bitmaps
// holds handles allocated by SetMenuIcons during buildMenu/processMenu, while
// impl.bitmap holds handles allocated by MenuItem.SetBitmap after the build.
// If a handle ever ended up in both, this function would double-free it
// (undefined behaviour on Win32 GDI), so callers must preserve the split.
func releaseMenuBitmaps(bitmaps []w32.HBITMAP, mapping map[int]*MenuItem) {
	for _, h := range bitmaps {
		w32.DeleteObject(w32.HGDIOBJ(h))
	}
	for _, item := range mapping {
		impl, ok := item.impl.(*windowsMenuItem)
		if !ok || impl.bitmap == 0 {
			continue
		}
		w32.DeleteObject(w32.HGDIOBJ(impl.bitmap))
		impl.bitmap = 0
	}
}

func (p *Win32Menu) freeBitmaps() {
	releaseMenuBitmaps(p.bitmaps, p.menuMapping)
	p.bitmaps = nil
}

func (p *Win32Menu) newMenu() w32.HMENU {
	if p.isPopup {
		return w32.NewPopupMenu()
	}
	return w32.CreateMenu()
}

// buildMenuBar populates the top level of an application menu. Those items form
// the menu bar strip, which MenuBarWndProc already draws, so they are left to
// the system. Everything hanging below them is a popup and is built by
// buildPopupMenu.
func (p *Win32Menu) buildMenuBar(parentMenu w32.HMENU, inputMenu *Menu) error {
	return p.buildMenuLevel(parentMenu, inputMenu, false)
}

// buildPopupMenu populates a dropdown, a context menu, or a nested submenu.
// Every menu below the bar is a popup, so its items are owner-drawn and take
// their colours from the window's theme rather than from uxtheme.
func (p *Win32Menu) buildPopupMenu(parentMenu w32.HMENU, inputMenu *Menu) error {
	return p.buildMenuLevel(parentMenu, inputMenu, true)
}

// buildMenuLevel populates parentMenu from inputMenu. Any native AppendMenu or
// SetMenuIcons failure returns an error; recursive submenu builds propagate
// the error so the outer Update can back out cleanly instead of attaching a
// half-built submenu via MF_POPUP.
//
// Call it through buildMenuBar or buildPopupMenu rather than directly, so the
// owner-draw decision is named at the call site.
func (p *Win32Menu) buildMenuLevel(parentMenu w32.HMENU, inputMenu *Menu, ownerDraw bool) error {
	currentRadioGroup := RadioGroup{}
	for _, item := range inputMenu.items {
		p.currentMenuID++
		itemID := p.currentMenuID
		p.menuMapping[itemID] = item

		menuItemImpl := newMenuItemImpl(item, parentMenu, itemID)
		menuItemImpl.parent = inputMenu
		menuItemImpl.ownerDraw = ownerDraw
		item.impl = menuItemImpl

		if item.Hidden() {
			if item.accelerator != nil {
				if p.parentWindow != nil {
					// Remove the accelerator from the keybindings
					p.parentWindow.parent.removeMenuBinding(item.accelerator)
				} else {
					// Remove the global keybindings
					globalApplication.KeyBinding.Remove(item.accelerator.String())
				}
			}
		}

		flags := uint32(w32.MF_STRING)
		// Popup items are owner-drawn so wails controls the text colour rather
		// than inheriting it from uxtheme, which follows the system light/dark
		// setting. The menu bar keeps its existing UAH drawing.
		if ownerDraw {
			flags = flags | w32.MF_OWNERDRAW
		}
		if item.disabled {
			flags = flags | w32.MF_GRAYED
		}
		if item.checked {
			flags = flags | w32.MF_CHECKED
		}
		if item.IsSeparator() {
			flags = flags | w32.MF_SEPARATOR
		}

		if item.checked && item.IsRadio() {
			flags = flags | w32.MFT_RADIOCHECK
		}

		if item.IsCheckbox() {
			p.checkboxItems[item] = append(p.checkboxItems[item], itemID)
		}
		if item.IsRadio() {
			currentRadioGroup.Add(itemID, item)
		} else {
			if len(currentRadioGroup) > 0 {
				for _, radioMember := range currentRadioGroup {
					currentRadioGroup := currentRadioGroup
					p.radioGroups[radioMember.MenuItem] = append(p.radioGroups[radioMember.MenuItem], &currentRadioGroup)
				}
				currentRadioGroup = RadioGroup{}
			}
		}

		if item.submenu != nil {
			flags = flags | w32.MF_POPUP
			newSubmenu := p.newMenu()
			if err := p.buildPopupMenu(newSubmenu, item.submenu); err != nil {
				// Submenu was allocated but never attached via AppendMenu, so
				// the outer DestroyMenu on parentMenu won't reach it. Free it
				// here to avoid leaking the HMENU.
				w32.DestroyMenu(newSubmenu)
				return err
			}
			itemID = int(newSubmenu)
			menuItemImpl.submenu = newSubmenu
		}

		var menuText = item.Label()
		if item.accelerator != nil {
			menuText = menuText + "\t" + item.accelerator.String()
			if item.callback != nil {
				if p.parentWindow != nil {
					p.parentWindow.parent.addMenuBinding(item.accelerator, item)
				} else {
					globalApplication.KeyBinding.Add(item.accelerator.String(), func(w Window) {
						item.handleClick()
					})
				}
			}
		}

		// If the item is hidden, don't append
		if item.Hidden() {
			continue
		}

		// Record the item under the identifier AppendMenu is about to be given.
		// For an MF_POPUP item that is the submenu's HMENU rather than the
		// command id, and WM_DRAWITEM/WM_MEASUREITEM report the same value, so
		// this is the only key an owner-draw lookup can use.
		p.drawMapping[itemID] = item

		ok := w32.AppendMenu(parentMenu, flags, uintptr(itemID), w32.MustStringToUTF16Ptr(menuText))
		if !ok {
			return fmt.Errorf("AppendMenu failed for %q: %v", menuText, syscall.GetLastError())
		}
		if item.bitmap != nil {
			handles, err := w32.SetMenuIcons(parentMenu, itemID, item.bitmap, nil)
			if err != nil {
				return fmt.Errorf("SetMenuIcons failed for %q: %w", menuText, err)
			}
			p.bitmaps = append(p.bitmaps, handles...)
		}
	}
	if len(currentRadioGroup) > 0 {
		for _, radioMember := range currentRadioGroup {
			currentRadioGroup := currentRadioGroup
			p.radioGroups[radioMember.MenuItem] = append(p.radioGroups[radioMember.MenuItem], &currentRadioGroup)
		}
		currentRadioGroup = RadioGroup{}
	}
	return nil
}

func (p *Win32Menu) Update() {
	// Stage the rebuild into a fresh HMENU and fresh maps, only swapping the
	// old state out once buildMenu returns successfully. On failure, the
	// previous menu (if any) stays displayed and usable. Note: buildMenu
	// replaces item.impl during the build, so on failure any MenuItem that
	// was processed before the failure point will hold a stale impl pointing
	// at the destroyed partial HMENU. Runtime mutations like SetBitmap on
	// such items will no-op until the next successful Update — acceptable
	// for a path that only triggers on Win32 resource exhaustion.
	newHMENU := p.newMenu()
	oldHMENU := p.menu
	oldMapping := p.menuMapping
	oldDrawMapping := p.drawMapping
	oldCheckboxes := p.checkboxItems
	oldRadios := p.radioGroups
	oldBitmaps := p.bitmaps

	// Transfer runtime SetBitmap handles off the old impls now, before
	// buildMenu reassigns item.impl and makes them unreachable via the
	// mapping walk. Every handle lives in oldBitmaps from here on.
	for _, item := range oldMapping {
		if impl, ok := item.impl.(*windowsMenuItem); ok && impl.bitmap != 0 {
			oldBitmaps = append(oldBitmaps, impl.bitmap)
			impl.bitmap = 0
		}
	}

	p.menu = newHMENU
	p.menuMapping = make(map[int]*MenuItem)
	p.drawMapping = make(map[int]*MenuItem)
	p.checkboxItems = make(map[*MenuItem][]int)
	p.radioGroups = make(map[*MenuItem][]*RadioGroup)
	p.currentMenuID = MenuItemMsgID
	p.bitmaps = nil

	// A context menu is a popup in its own right; an application menu starts at
	// the bar, and only what hangs below it is a popup.
	build := p.buildMenuBar
	if p.isPopup {
		build = p.buildPopupMenu
	}

	if err := build(newHMENU, p.menuData); err != nil {
		globalApplication.error("menu rebuild failed, keeping previous menu: %v", err)
		// Release bitmaps allocated during the partial build, destroy the
		// partial HMENU, then restore the previous state.
		p.freeBitmaps()
		w32.DestroyMenu(newHMENU)
		p.menu = oldHMENU
		p.menuMapping = oldMapping
		p.drawMapping = oldDrawMapping
		p.checkboxItems = oldCheckboxes
		p.radioGroups = oldRadios
		p.bitmaps = oldBitmaps
		return
	}

	// Success: release the previous menu's bitmaps and HMENU tree.
	if oldHMENU != 0 {
		releaseMenuBitmaps(oldBitmaps, oldMapping)
		w32.DestroyMenu(oldHMENU)
	}
	p.updateRadioGroups()
}

func NewPopupMenu(parent w32.HWND, inputMenu *Menu) *Win32Menu {
	result := &Win32Menu{
		isPopup:       true,
		parent:        parent,
		menuData:      inputMenu,
		checkboxItems: make(map[*MenuItem][]int),
		radioGroups:   make(map[*MenuItem][]*RadioGroup),
	}
	result.Update()
	return result
}
func NewApplicationMenu(parent *windowsWebviewWindow, inputMenu *Menu) *Win32Menu {
	result := &Win32Menu{
		parentWindow:  parent,
		parent:        parent.hwnd,
		menuData:      inputMenu,
		checkboxItems: make(map[*MenuItem][]int),
		radioGroups:   make(map[*MenuItem][]*RadioGroup),
	}
	result.Update()
	return result
}

func (p *Win32Menu) ShowAt(x int, y int) {
	// Prevent concurrent menu displays - TrackPopupMenuEx is blocking and
	// calling it while another popup is showing causes "TrackPopupMenu failed"
	if !p.isShowing.CompareAndSwap(false, true) {
		return
	}
	defer p.isShowing.Store(false)

	w32.SetForegroundWindow(p.parent)

	if p.onMenuOpen != nil {
		p.onMenuOpen()
	}

	// Get screen dimensions to determine menu positioning
	monitor := w32.MonitorFromWindow(p.parent, w32.MONITOR_DEFAULTTONEAREST)
	var monitorInfo w32.MONITORINFO
	monitorInfo.CbSize = uint32(unsafe.Sizeof(monitorInfo))
	if !w32.GetMonitorInfo(monitor, &monitorInfo) {
		globalApplication.fatal("GetMonitorInfo failed")
	}

	// Set flags to always position the menu above the cursor
	menuFlags := uint32(w32.TPM_LEFTALIGN | w32.TPM_BOTTOMALIGN)

	// Check if we're close to the right edge of the screen
	// If so, right-align the menu with some padding
	if x > int(monitorInfo.RcWork.Right)-200 { // Assuming 200px as a reasonable menu width
		menuFlags = uint32(w32.TPM_RIGHTALIGN | w32.TPM_BOTTOMALIGN)
		// Add a small padding (10px) from the right edge
		x = int(monitorInfo.RcWork.Right) - 10
	}

	if !w32.TrackPopupMenuEx(p.menu, menuFlags, int32(x), int32(y), p.parent, nil) {
		// TrackPopupMenuEx can fail if called during menu transitions or rapid clicks.
		// This is not fatal - just skip this menu display attempt.
		globalApplication.debug("TrackPopupMenu failed - menu may already be showing")
		return
	}

	if p.onMenuClose != nil {
		p.onMenuClose()
	}

	if !w32.PostMessage(p.parent, w32.WM_NULL, 0, 0) {
		globalApplication.fatal("PostMessage failed")
	}

}

func (p *Win32Menu) ShowAtCursor() {
	x, y, ok := w32.GetCursorPos()
	if !ok {
		// GetCursorPos fails when the process is not on the interactive input
		// desktop (locked / secure desktop). A context menu simply cannot be
		// shown then - skip it, do NOT kill the app. Matches the graceful
		// handling in windowsMenu.ShowAtCursor.
		globalApplication.debug("GetCursorPos failed - cannot show context menu at cursor")
		return
	}

	p.ShowAt(x, y)
}

func (p *Win32Menu) ProcessCommand(cmdMsgID int) bool {
	item := p.menuMapping[cmdMsgID]
	if item == nil {
		return false
	}
	if item.IsRadio() {
		if item.checked {
			return true
		}
		item.checked = true
		p.updateRadioGroup(item)
	}
	if item.callback != nil {
		item.handleClick()
	}
	return true
}

func (p *Win32Menu) Destroy() {
	p.freeBitmaps()
	w32.DestroyMenu(p.menu)
}

func (p *Win32Menu) UpdateMenuItem(item *MenuItem) {
	if item.IsCheckbox() {
		for _, itemID := range p.checkboxItems[item] {
			var checkState uint = w32.MF_UNCHECKED
			if item.checked {
				checkState = w32.MF_CHECKED
			}
			w32.CheckMenuItem(p.menu, uintptr(itemID), checkState)
		}
		return
	}
	if item.IsRadio() && item.checked == true {
		p.updateRadioGroup(item)
	}
}

func (p *Win32Menu) updateRadioGroups() {
	for menuItem := range p.radioGroups {
		if menuItem.checked {
			p.updateRadioGroup(menuItem)
		}
	}
}

func (p *Win32Menu) updateRadioGroup(item *MenuItem) {
	for _, radioGroup := range p.radioGroups[item] {
		thisMenuID := radioGroup.MenuID(item)
		startID, endID := radioGroup.Bounds()
		w32.CheckRadio(p.menu, startID, endID, thisMenuID)

	}
}

func (p *Win32Menu) OnMenuOpen(fn func()) {
	p.onMenuOpen = fn
}

func (p *Win32Menu) OnMenuClose(fn func()) {
	p.onMenuClose = fn
}
