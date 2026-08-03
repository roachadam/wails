//go:build windows

package w32

import (
	"unsafe"
)

// Menu theme part ids from vsstyle.h. MENU_POPUPITEM and MENU_BARITEM are
// declared in menubar.go.
const (
	MENU_POPUPBACKGROUND      = 9
	MENU_POPUPBORDERS         = 10
	MENU_POPUPCHECK           = 11
	MENU_POPUPCHECKBACKGROUND = 12
	MENU_POPUPGUTTER          = 13
	MENU_POPUPSEPARATOR       = 15
	MENU_POPUPSUBMENU         = 16
)

// MENU_POPUPITEM states.
const (
	MPI_NORMAL      = 1
	MPI_HOT         = 2
	MPI_DISABLED    = 3
	MPI_DISABLEDHOT = 4
)

// MENU_POPUPCHECK states.
const (
	MC_CHECKMARKNORMAL   = 1
	MC_CHECKMARKDISABLED = 2
	MC_BULLETNORMAL      = 3
	MC_BULLETDISABLED    = 4
)

// MENU_POPUPCHECKBACKGROUND states.
const (
	MCB_DISABLED = 1
	MCB_NORMAL   = 2
	MCB_BITMAP   = 3
)

// MENU_POPUPSUBMENU states.
const (
	MSM_NORMAL   = 1
	MSM_DISABLED = 2
)

// MN_GETHMENU asks an open menu's window - class #32768 - for the HMENU it is
// displaying. It is the only way to reach a popup's handle from outside the
// menu loop.
const MN_GETHMENU = 0x01E1

// THEMESIZE values for GetThemePartSize.
const (
	TS_MIN  = 0 // minimum size
	TS_TRUE = 1 // size of the part's image, unstretched
	TS_DRAW = 2 // size the part will actually be drawn at
)

// Theme property ids for GetThemeMargins.
const (
	TMT_SIZINGMARGINS  = 3601
	TMT_CONTENTMARGINS = 3602
)

var (
	procGetThemePartSize = uxtheme.NewProc("GetThemePartSize")
	procGetThemeMargins  = uxtheme.NewProc("GetThemeMargins")
)

// GetThemePartSize returns the size of a themed part.
//
// These are the metrics the visual style actually draws with. GetSystemMetrics
// returns the classic pre-theme values, which for menus are materially smaller,
// so anything laying out themed menu items has to ask the theme instead.
//
// Reports false if the theme does not supply the part, which is the case under
// the classic style and in High Contrast mode; callers need a fallback.
func GetThemePartSize(hTheme HTHEME, hdc HDC, iPartId, iStateId int32, eSize int32) (SIZE, bool) {
	var sz SIZE
	ret, _, _ := procGetThemePartSize.Call(
		uintptr(hTheme),
		uintptr(hdc),
		uintptr(iPartId),
		uintptr(iStateId),
		0, // prc: NULL, meaning the part's own size rather than one fitted to a rect
		uintptr(eSize),
		uintptr(unsafe.Pointer(&sz)),
	)
	// S_OK is 0.
	if ret != 0 {
		return SIZE{}, false
	}
	return sz, true
}

// GetThemeMargins returns a margin property of a themed part, typically
// TMT_CONTENTMARGINS - the padding between a part's bounds and its content.
//
// Reports false when the theme does not supply the property.
func GetThemeMargins(hTheme HTHEME, hdc HDC, iPartId, iStateId, iPropId int32) (MARGINS, bool) {
	var m MARGINS
	ret, _, _ := procGetThemeMargins.Call(
		uintptr(hTheme),
		uintptr(hdc),
		uintptr(iPartId),
		uintptr(iStateId),
		uintptr(iPropId),
		0, // prc: NULL
		uintptr(unsafe.Pointer(&m)),
	)
	if ret != 0 {
		return MARGINS{}, false
	}
	return m, true
}

// DrawThemeBackground paints a themed part into hdc.
//
// It paints in the visual style's own colours, so it cannot be used to draw a
// menu glyph that has to follow wails' palette. It is still the only way to see
// what the visual style would have drawn, which is what sizing a replacement
// glyph correctly depends on.
func DrawThemeBackground(hTheme HTHEME, hdc HDC, iPartId, iStateId int32, prc *RECT) bool {
	ret, _, _ := procDrawThemeBackground.Call(
		uintptr(hTheme),
		uintptr(hdc),
		uintptr(iPartId),
		uintptr(iStateId),
		uintptr(unsafe.Pointer(prc)),
		0, // prcClip: NULL
	)
	// S_OK is 0.
	return ret == 0
}

var procOpenThemeDataForDpi = uxtheme.NewProc("OpenThemeDataForDpi")

// HasOpenThemeDataForDpiFunc reports whether the DPI-aware theme handle is
// available. It shipped in Windows 10 1703.
func HasOpenThemeDataForDpiFunc() bool {
	return procOpenThemeDataForDpi.Find() == nil
}

// OpenThemeDataForDpi opens a theme whose metrics are expressed for dpi rather
// than for the window's own.
//
// OpenThemeData returns part sizes that follow the display scaling but margins
// that do not - they come back in 96-DPI units at every scaling factor, which
// makes a menu laid out from them too tight at 200%. This is the documented way
// to ask for metrics at a specific DPI.
//
// Returns 0 when unavailable or when the class has no theme.
func OpenThemeDataForDpi(hwnd HWND, classList string, dpi UINT) HTHEME {
	if procOpenThemeDataForDpi.Find() != nil {
		return 0
	}
	ret, _, _ := procOpenThemeDataForDpi.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(MustStringToUTF16Ptr(classList))),
		uintptr(dpi),
	)
	return HTHEME(ret)
}
