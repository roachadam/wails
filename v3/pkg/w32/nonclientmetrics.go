//go:build windows

package w32

import (
	"unsafe"
)

const (
	SPI_GETNONCLIENTMETRICS = 0x0029

	// USER_DEFAULT_SCREEN_DPI is the DPI that unscaled metrics are expressed in.
	USER_DEFAULT_SCREEN_DPI = 96
)

// NONCLIENTMETRICS holds the metrics Windows uses for non-client areas, which
// includes the fonts for menus, captions, message boxes and status bars.
//
// https://learn.microsoft.com/en-us/windows/win32/api/winuser/ns-winuser-nonclientmetricsw
//
// PaddedBorderWidth was added in Windows Vista. Callers must set CbSize before
// use; SystemParametersInfo rejects a size it does not recognise, which is how
// it distinguishes the two layouts.
type NONCLIENTMETRICS struct {
	CbSize            uint32
	BorderWidth       int32
	ScrollWidth       int32
	ScrollHeight      int32
	CaptionWidth      int32
	CaptionHeight     int32
	CaptionFont       LOGFONT
	SmCaptionWidth    int32
	SmCaptionHeight   int32
	SmCaptionFont     LOGFONT
	MenuWidth         int32
	MenuHeight        int32
	MenuFont          LOGFONT
	StatusFont        LOGFONT
	MessageFont       LOGFONT
	PaddedBorderWidth int32
}

var procSystemParametersInfoForDpi = moduser32.NewProc("SystemParametersInfoForDpiW")

// HasSystemParametersInfoForDpiFunc reports whether the DPI-aware variant is
// available. It shipped in Windows 10 1607; on older builds callers fall back to
// SystemParametersInfo, which returns metrics for the primary display's DPI.
func HasSystemParametersInfoForDpiFunc() bool {
	return procSystemParametersInfoForDpi.Find() == nil
}

// GetNonClientMetricsForDpi returns the non-client metrics scaled for dpi.
//
// Pass a DPI from GetDpiForWindow so a window on a secondary display gets that
// display's metrics rather than the primary's. When the DPI-aware entry point is
// unavailable the unscaled call is used instead, which is correct at 96 DPI and
// the best available answer below Windows 10 1607.
func GetNonClientMetricsForDpi(dpi UINT) (*NONCLIENTMETRICS, bool) {
	var ncm NONCLIENTMETRICS
	ncm.CbSize = uint32(unsafe.Sizeof(ncm))

	if dpi != 0 && HasSystemParametersInfoForDpiFunc() {
		ret, _, _ := procSystemParametersInfoForDpi.Call(
			uintptr(SPI_GETNONCLIENTMETRICS),
			uintptr(ncm.CbSize),
			uintptr(unsafe.Pointer(&ncm)),
			0,
			uintptr(dpi),
		)
		if ret != 0 {
			return &ncm, true
		}
		// Fall through: a failed DPI-aware call may still succeed unscaled.
		ncm.CbSize = uint32(unsafe.Sizeof(ncm))
	}

	ret, _, _ := procSystemParametersInfo.Call(
		uintptr(SPI_GETNONCLIENTMETRICS),
		uintptr(ncm.CbSize),
		uintptr(unsafe.Pointer(&ncm)),
		0,
	)
	if ret == 0 {
		return nil, false
	}
	return &ncm, true
}

// SystemMetricForDpi returns GetSystemMetrics(index) scaled for dpi, falling
// back to the unscaled value when the DPI-aware entry point is unavailable.
//
// The result is returned as-is rather than being checked for 0. Zero is a
// legitimate value for several metrics, so treating it as failure would
// silently substitute the primary display's unscaled value for a correct
// scaled one. Availability of the entry point is the only thing worth testing.
func SystemMetricForDpi(index int, dpi UINT) int {
	if dpi != 0 && HasGetSystemMetricsForDpiFunc() {
		return GetSystemMetricsForDpi(index, dpi)
	}
	return GetSystemMetrics(index)
}
