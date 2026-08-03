//go:build windows

/*
 * Copyright (C) 2019 The Winc Authors. All Rights Reserved.
 * Copyright (C) 2010-2012 The W32 Authors. All Rights Reserved.
 */
package w32

import (
	"syscall"
	"unsafe"
)

var (
	modgdi32 = syscall.NewLazyDLL("gdi32.dll")

	procGetDeviceCaps             = modgdi32.NewProc("GetDeviceCaps")
	procDeleteObject              = modgdi32.NewProc("DeleteObject")
	procCreateFontIndirect        = modgdi32.NewProc("CreateFontIndirectW")
	procGetTextFace               = modgdi32.NewProc("GetTextFaceW")
	procGetGlyphIndices           = modgdi32.NewProc("GetGlyphIndicesW")
	procGetGlyphOutline           = modgdi32.NewProc("GetGlyphOutlineW")
	procAbortDoc                  = modgdi32.NewProc("AbortDoc")
	procBitBlt                    = modgdi32.NewProc("BitBlt")
	procPatBlt                    = modgdi32.NewProc("PatBlt")
	procCloseEnhMetaFile          = modgdi32.NewProc("CloseEnhMetaFile")
	procCopyEnhMetaFile           = modgdi32.NewProc("CopyEnhMetaFileW")
	procCreateBrushIndirect       = modgdi32.NewProc("CreateBrushIndirect")
	procCreateCompatibleDC        = modgdi32.NewProc("CreateCompatibleDC")
	procCreateDC                  = modgdi32.NewProc("CreateDCW")
	procCreateDIBSection          = modgdi32.NewProc("CreateDIBSection")
	procCreateEnhMetaFile         = modgdi32.NewProc("CreateEnhMetaFileW")
	procCreateIC                  = modgdi32.NewProc("CreateICW")
	procDeleteDC                  = modgdi32.NewProc("DeleteDC")
	procDeleteEnhMetaFile         = modgdi32.NewProc("DeleteEnhMetaFile")
	procEllipse                   = modgdi32.NewProc("Ellipse")
	procEndDoc                    = modgdi32.NewProc("EndDoc")
	procEndPage                   = modgdi32.NewProc("EndPage")
	procExtCreatePen              = modgdi32.NewProc("ExtCreatePen")
	procGetEnhMetaFile            = modgdi32.NewProc("GetEnhMetaFileW")
	procGetEnhMetaFileHeader      = modgdi32.NewProc("GetEnhMetaFileHeader")
	procGetObject                 = modgdi32.NewProc("GetObjectW")
	procGetStockObject            = modgdi32.NewProc("GetStockObject")
	procGetTextExtentExPoint      = modgdi32.NewProc("GetTextExtentExPointW")
	procGetTextExtentPoint32      = modgdi32.NewProc("GetTextExtentPoint32W")
	procGetTextMetrics            = modgdi32.NewProc("GetTextMetricsW")
	procLineTo                    = modgdi32.NewProc("LineTo")
	procMoveToEx                  = modgdi32.NewProc("MoveToEx")
	procPlayEnhMetaFile           = modgdi32.NewProc("PlayEnhMetaFile")
	procRectangle                 = modgdi32.NewProc("Rectangle")
	procResetDC                   = modgdi32.NewProc("ResetDCW")
	procSelectObject              = modgdi32.NewProc("SelectObject")
	procSetBkMode                 = modgdi32.NewProc("SetBkMode")
	procSetBrushOrgEx             = modgdi32.NewProc("SetBrushOrgEx")
	procSetStretchBltMode         = modgdi32.NewProc("SetStretchBltMode")
	procSetTextColor              = modgdi32.NewProc("SetTextColor")
	procSetBkColor                = modgdi32.NewProc("SetBkColor")
	procStartDoc                  = modgdi32.NewProc("StartDocW")
	procStartPage                 = modgdi32.NewProc("StartPage")
	procStretchBlt                = modgdi32.NewProc("StretchBlt")
	procSetDIBitsToDevice         = modgdi32.NewProc("SetDIBitsToDevice")
	procChoosePixelFormat         = modgdi32.NewProc("ChoosePixelFormat")
	procDescribePixelFormat       = modgdi32.NewProc("DescribePixelFormat")
	procGetEnhMetaFilePixelFormat = modgdi32.NewProc("GetEnhMetaFilePixelFormat")
	procGetPixelFormat            = modgdi32.NewProc("GetPixelFormat")
	procSetPixelFormat            = modgdi32.NewProc("SetPixelFormat")
	procSwapBuffers               = modgdi32.NewProc("SwapBuffers")
	procSaveDC                    = modgdi32.NewProc("SaveDC")
	procRestoreDC                 = modgdi32.NewProc("RestoreDC")
	procSelectClipRgn             = modgdi32.NewProc("SelectClipRgn")
	procExcludeClipRect           = modgdi32.NewProc("ExcludeClipRect")
	procExtTextOut                = modgdi32.NewProc("ExtTextOutW")
)

func GetDeviceCaps(hdc HDC, index int) int {
	ret, _, _ := procGetDeviceCaps.Call(
		uintptr(hdc),
		uintptr(index))

	return int(ret)
}

func DeleteObject(hObject HGDIOBJ) bool {
	ret, _, _ := procDeleteObject.Call(
		uintptr(hObject))

	return ret != 0
}

func CreateFontIndirect(logFont *LOGFONT) HFONT {
	ret, _, _ := procCreateFontIndirect.Call(
		uintptr(unsafe.Pointer(logFont)))

	return HFONT(ret)
}

func AbortDoc(hdc HDC) int {
	ret, _, _ := procAbortDoc.Call(
		uintptr(hdc))

	return int(ret)
}

func BitBlt(hdcDest HDC, nXDest, nYDest, nWidth, nHeight int, hdcSrc HDC, nXSrc, nYSrc int, dwRop uint) {
	ret, _, _ := procBitBlt.Call(
		uintptr(hdcDest),
		uintptr(nXDest),
		uintptr(nYDest),
		uintptr(nWidth),
		uintptr(nHeight),
		uintptr(hdcSrc),
		uintptr(nXSrc),
		uintptr(nYSrc),
		uintptr(dwRop))

	if ret == 0 {
		panic("BitBlt failed")
	}
}

func PatBlt(hdc HDC, nXLeft, nYLeft, nWidth, nHeight int, dwRop uint) {
	ret, _, _ := procPatBlt.Call(
		uintptr(hdc),
		uintptr(nXLeft),
		uintptr(nYLeft),
		uintptr(nWidth),
		uintptr(nHeight),
		uintptr(dwRop))

	if ret == 0 {
		panic("PatBlt failed")
	}
}

func CloseEnhMetaFile(hdc HDC) HENHMETAFILE {
	ret, _, _ := procCloseEnhMetaFile.Call(
		uintptr(hdc))

	return HENHMETAFILE(ret)
}

func CopyEnhMetaFile(hemfSrc HENHMETAFILE, lpszFile *uint16) HENHMETAFILE {
	ret, _, _ := procCopyEnhMetaFile.Call(
		uintptr(hemfSrc),
		uintptr(unsafe.Pointer(lpszFile)))

	return HENHMETAFILE(ret)
}

func CreateBrushIndirect(lplb *LOGBRUSH) HBRUSH {
	ret, _, _ := procCreateBrushIndirect.Call(
		uintptr(unsafe.Pointer(lplb)))

	return HBRUSH(ret)
}

func CreateCompatibleDC(hdc HDC) HDC {
	ret, _, _ := procCreateCompatibleDC.Call(
		uintptr(hdc))

	if ret == 0 {
		panic("Create compatible DC failed")
	}

	return HDC(ret)
}

func CreateDC(lpszDriver, lpszDevice, lpszOutput *uint16, lpInitData *DEVMODE) HDC {
	ret, _, _ := procCreateDC.Call(
		uintptr(unsafe.Pointer(lpszDriver)),
		uintptr(unsafe.Pointer(lpszDevice)),
		uintptr(unsafe.Pointer(lpszOutput)),
		uintptr(unsafe.Pointer(lpInitData)))

	return HDC(ret)
}

func CreateDIBSection(hdc HDC, pbmi *BITMAPINFO, iUsage uint, ppvBits *unsafe.Pointer, hSection HANDLE, dwOffset uint) HBITMAP {
	ret, _, _ := procCreateDIBSection.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(pbmi)),
		uintptr(iUsage),
		uintptr(unsafe.Pointer(ppvBits)),
		uintptr(hSection),
		uintptr(dwOffset))

	return HBITMAP(ret)
}

func CreateEnhMetaFile(hdcRef HDC, lpFilename *uint16, lpRect *RECT, lpDescription *uint16) HDC {
	ret, _, _ := procCreateEnhMetaFile.Call(
		uintptr(hdcRef),
		uintptr(unsafe.Pointer(lpFilename)),
		uintptr(unsafe.Pointer(lpRect)),
		uintptr(unsafe.Pointer(lpDescription)))

	return HDC(ret)
}

func CreateIC(lpszDriver, lpszDevice, lpszOutput *uint16, lpdvmInit *DEVMODE) HDC {
	ret, _, _ := procCreateIC.Call(
		uintptr(unsafe.Pointer(lpszDriver)),
		uintptr(unsafe.Pointer(lpszDevice)),
		uintptr(unsafe.Pointer(lpszOutput)),
		uintptr(unsafe.Pointer(lpdvmInit)))

	return HDC(ret)
}

func DeleteDC(hdc HDC) bool {
	ret, _, _ := procDeleteDC.Call(
		uintptr(hdc))

	return ret != 0
}

func DeleteEnhMetaFile(hemf HENHMETAFILE) bool {
	ret, _, _ := procDeleteEnhMetaFile.Call(
		uintptr(hemf))

	return ret != 0
}

func Ellipse(hdc HDC, nLeftRect, nTopRect, nRightRect, nBottomRect int32) bool {
	ret, _, _ := procEllipse.Call(
		uintptr(hdc),
		uintptr(nLeftRect),
		uintptr(nTopRect),
		uintptr(nRightRect),
		uintptr(nBottomRect))

	return ret != 0
}

func EndDoc(hdc HDC) int {
	ret, _, _ := procEndDoc.Call(
		uintptr(hdc))

	return int(ret)
}

func EndPage(hdc HDC) int {
	ret, _, _ := procEndPage.Call(
		uintptr(hdc))

	return int(ret)
}

func ExtCreatePen(dwPenStyle, dwWidth uint, lplb *LOGBRUSH, dwStyleCount uint, lpStyle *uint) HPEN {
	ret, _, _ := procExtCreatePen.Call(
		uintptr(dwPenStyle),
		uintptr(dwWidth),
		uintptr(unsafe.Pointer(lplb)),
		uintptr(dwStyleCount),
		uintptr(unsafe.Pointer(lpStyle)))

	return HPEN(ret)
}

func GetEnhMetaFile(lpszMetaFile *uint16) HENHMETAFILE {
	ret, _, _ := procGetEnhMetaFile.Call(
		uintptr(unsafe.Pointer(lpszMetaFile)))

	return HENHMETAFILE(ret)
}

func GetEnhMetaFileHeader(hemf HENHMETAFILE, cbBuffer uint, lpemh *ENHMETAHEADER) uint {
	ret, _, _ := procGetEnhMetaFileHeader.Call(
		uintptr(hemf),
		uintptr(cbBuffer),
		uintptr(unsafe.Pointer(lpemh)))

	return uint(ret)
}

func GetObject(hgdiobj HGDIOBJ, cbBuffer uintptr, lpvObject unsafe.Pointer) int {
	ret, _, _ := procGetObject.Call(
		uintptr(hgdiobj),
		uintptr(cbBuffer),
		uintptr(lpvObject))

	return int(ret)
}

func GetStockObject(fnObject int) HGDIOBJ {
	ret, _, _ := procGetDeviceCaps.Call(
		uintptr(fnObject))

	return HGDIOBJ(ret)
}

func GetTextExtentExPoint(hdc HDC, lpszStr *uint16, cchString, nMaxExtent int, lpnFit, alpDx *int, lpSize *SIZE) bool {
	ret, _, _ := procGetTextExtentExPoint.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(lpszStr)),
		uintptr(cchString),
		uintptr(nMaxExtent),
		uintptr(unsafe.Pointer(lpnFit)),
		uintptr(unsafe.Pointer(alpDx)),
		uintptr(unsafe.Pointer(lpSize)))

	return ret != 0
}

func GetTextExtentPoint32(hdc HDC, lpString *uint16, c int, lpSize *SIZE) bool {
	ret, _, _ := procGetTextExtentPoint32.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(lpString)),
		uintptr(c),
		uintptr(unsafe.Pointer(lpSize)))

	return ret != 0
}

func GetTextMetrics(hdc HDC, lptm *TEXTMETRIC) bool {
	ret, _, _ := procGetTextMetrics.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(lptm)))

	return ret != 0
}

func LineTo(hdc HDC, nXEnd, nYEnd int32) bool {
	ret, _, _ := procLineTo.Call(
		uintptr(hdc),
		uintptr(nXEnd),
		uintptr(nYEnd))

	return ret != 0
}

func MoveToEx(hdc HDC, x, y int, lpPoint *POINT) bool {
	ret, _, _ := procMoveToEx.Call(
		uintptr(hdc),
		uintptr(x),
		uintptr(y),
		uintptr(unsafe.Pointer(lpPoint)))

	return ret != 0
}

func PlayEnhMetaFile(hdc HDC, hemf HENHMETAFILE, lpRect *RECT) bool {
	ret, _, _ := procPlayEnhMetaFile.Call(
		uintptr(hdc),
		uintptr(hemf),
		uintptr(unsafe.Pointer(lpRect)))

	return ret != 0
}

func Rectangle(hdc HDC, nLeftRect, nTopRect, nRightRect, nBottomRect int32) bool {
	ret, _, _ := procRectangle.Call(
		uintptr(hdc),
		uintptr(nLeftRect),
		uintptr(nTopRect),
		uintptr(nRightRect),
		uintptr(nBottomRect))

	return ret != 0
}

func ResetDC(hdc HDC, lpInitData *DEVMODE) HDC {
	ret, _, _ := procResetDC.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(lpInitData)))

	return HDC(ret)
}

func SelectObject(hdc HDC, hgdiobj HGDIOBJ) HGDIOBJ {
	ret, _, _ := procSelectObject.Call(
		uintptr(hdc),
		uintptr(hgdiobj))

	if ret == 0 {
		panic("SelectObject failed")
	}

	return HGDIOBJ(ret)
}

func SetBkMode(hdc HDC, iBkMode int) int {
	ret, _, _ := procSetBkMode.Call(
		uintptr(hdc),
		uintptr(iBkMode))

	if ret == 0 {
		panic("SetBkMode failed")
	}

	return int(ret)
}

func SetBrushOrgEx(hdc HDC, nXOrg, nYOrg int, lppt *POINT) bool {
	ret, _, _ := procSetBrushOrgEx.Call(
		uintptr(hdc),
		uintptr(nXOrg),
		uintptr(nYOrg),
		uintptr(unsafe.Pointer(lppt)))

	return ret != 0
}

func SetStretchBltMode(hdc HDC, iStretchMode int) int {
	ret, _, _ := procSetStretchBltMode.Call(
		uintptr(hdc),
		uintptr(iStretchMode))

	return int(ret)
}

func SetTextColor(hdc HDC, crColor COLORREF) COLORREF {
	ret, _, _ := procSetTextColor.Call(
		uintptr(hdc),
		uintptr(crColor))

	if ret == CLR_INVALID {
		panic("SetTextColor failed")
	}

	return COLORREF(ret)
}

func SetBkColor(hdc HDC, crColor COLORREF) COLORREF {
	ret, _, _ := procSetBkColor.Call(
		uintptr(hdc),
		uintptr(crColor))

	if ret == CLR_INVALID {
		panic("SetBkColor failed")
	}

	return COLORREF(ret)
}

func StartDoc(hdc HDC, lpdi *DOCINFO) int {
	ret, _, _ := procStartDoc.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(lpdi)))

	return int(ret)
}

func StartPage(hdc HDC) int {
	ret, _, _ := procStartPage.Call(
		uintptr(hdc))

	return int(ret)
}

func StretchBlt(hdcDest HDC, nXOriginDest, nYOriginDest, nWidthDest, nHeightDest int, hdcSrc HDC, nXOriginSrc, nYOriginSrc, nWidthSrc, nHeightSrc int, dwRop uint) {
	ret, _, _ := procStretchBlt.Call(
		uintptr(hdcDest),
		uintptr(nXOriginDest),
		uintptr(nYOriginDest),
		uintptr(nWidthDest),
		uintptr(nHeightDest),
		uintptr(hdcSrc),
		uintptr(nXOriginSrc),
		uintptr(nYOriginSrc),
		uintptr(nWidthSrc),
		uintptr(nHeightSrc),
		uintptr(dwRop))

	if ret == 0 {
		panic("StretchBlt failed")
	}
}

func SetDIBitsToDevice(hdc HDC, xDest, yDest, dwWidth, dwHeight, xSrc, ySrc int, uStartScan, cScanLines uint, lpvBits []byte, lpbmi *BITMAPINFO, fuColorUse uint) int {
	ret, _, _ := procSetDIBitsToDevice.Call(
		uintptr(hdc),
		uintptr(xDest),
		uintptr(yDest),
		uintptr(dwWidth),
		uintptr(dwHeight),
		uintptr(xSrc),
		uintptr(ySrc),
		uintptr(uStartScan),
		uintptr(cScanLines),
		uintptr(unsafe.Pointer(&lpvBits[0])),
		uintptr(unsafe.Pointer(lpbmi)),
		uintptr(fuColorUse))

	return int(ret)
}

func ChoosePixelFormat(hdc HDC, pfd *PIXELFORMATDESCRIPTOR) int {
	ret, _, _ := procChoosePixelFormat.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(pfd)),
	)
	return int(ret)
}

func DescribePixelFormat(hdc HDC, iPixelFormat int, nBytes uint, pfd *PIXELFORMATDESCRIPTOR) int {
	ret, _, _ := procDescribePixelFormat.Call(
		uintptr(hdc),
		uintptr(iPixelFormat),
		uintptr(nBytes),
		uintptr(unsafe.Pointer(pfd)),
	)
	return int(ret)
}

func GetEnhMetaFilePixelFormat(hemf HENHMETAFILE, cbBuffer uint32, pfd *PIXELFORMATDESCRIPTOR) uint {
	ret, _, _ := procGetEnhMetaFilePixelFormat.Call(
		uintptr(hemf),
		uintptr(cbBuffer),
		uintptr(unsafe.Pointer(pfd)),
	)
	return uint(ret)
}

func GetPixelFormat(hdc HDC) int {
	ret, _, _ := procGetPixelFormat.Call(
		uintptr(hdc),
	)
	return int(ret)
}

func SetPixelFormat(hdc HDC, iPixelFormat int, pfd *PIXELFORMATDESCRIPTOR) bool {
	ret, _, _ := procSetPixelFormat.Call(
		uintptr(hdc),
		uintptr(iPixelFormat),
		uintptr(unsafe.Pointer(pfd)),
	)
	return ret == TRUE
}

func SwapBuffers(hdc HDC) bool {
	ret, _, _ := procSwapBuffers.Call(uintptr(hdc))
	return ret == TRUE
}

func SaveDC(hdc HDC) int {
	ret, _, _ := procSaveDC.Call(uintptr(hdc))
	return int(ret)
}

func RestoreDC(hdc HDC, nSavedDC int) bool {
	ret, _, _ := procRestoreDC.Call(
		uintptr(hdc),
		uintptr(nSavedDC))
	return ret != 0
}

func SelectClipRgn(hdc HDC, hrgn HRGN) int {
	ret, _, _ := procSelectClipRgn.Call(
		uintptr(hdc),
		uintptr(hrgn))
	return int(ret)
}

func ExcludeClipRect(hdc HDC, left, top, right, bottom int32) int {
	ret, _, _ := procExcludeClipRect.Call(
		uintptr(hdc),
		uintptr(left),
		uintptr(top),
		uintptr(right),
		uintptr(bottom))
	return int(ret)
}

func ExtTextOut(hdc HDC, x, y int32, fuOptions uint32, lprc *RECT, lpString *uint16, cbCount uint32, lpDx *int) bool {
	var rectPtr uintptr
	if lprc != nil {
		rectPtr = uintptr(unsafe.Pointer(lprc))
	}
	var dxPtr uintptr
	if lpDx != nil {
		dxPtr = uintptr(unsafe.Pointer(lpDx))
	}
	ret, _, _ := procExtTextOut.Call(
		uintptr(hdc),
		uintptr(x),
		uintptr(y),
		uintptr(fuOptions),
		rectPtr,
		uintptr(unsafe.Pointer(lpString)),
		uintptr(cbCount),
		dxPtr)
	return ret != 0
}

// GetTextFace returns the typeface name of the font currently selected into hdc.
//
// GDI substitutes silently when a requested face is not installed, so this is
// the only way to find out whether the font that got created is the font that
// was asked for.
func GetTextFace(hdc HDC) string {
	var name [32]uint16 // LF_FACESIZE
	ret, _, _ := procGetTextFace.Call(
		uintptr(hdc),
		uintptr(len(name)),
		uintptr(unsafe.Pointer(&name[0])),
	)
	if ret == 0 {
		return ""
	}
	return syscall.UTF16ToString(name[:])
}

// GGI_MARK_NONEXISTING_GLYPHS makes GetGlyphIndices report 0xffff for a
// character the font has no glyph for, rather than the default glyph.
const GGI_MARK_NONEXISTING_GLYPHS = 1

// FontHasGlyphs reports whether the font selected into hdc can draw every
// character of s.
//
// This is the question worth asking before using an icon font. Comparing
// typeface names does not answer it - GDI reports back the name that was
// requested - and a font that is present but lacks the codepoint draws a
// missing-glyph box, which looks like a bug rather than a fallback.
func FontHasGlyphs(hdc HDC, s string) bool {
	chars := MustStringToUTF16(s)
	chars = chars[:len(chars)-1] // drop the terminating NUL
	if len(chars) == 0 {
		return false
	}

	indices := make([]uint16, len(chars))
	ret, _, _ := procGetGlyphIndices.Call(
		uintptr(hdc),
		uintptr(unsafe.Pointer(&chars[0])),
		uintptr(len(chars)),
		uintptr(unsafe.Pointer(&indices[0])),
		GGI_MARK_NONEXISTING_GLYPHS,
	)
	if ret == 0xffffffff { // GDI_ERROR
		return false
	}
	for _, i := range indices {
		if i == 0xffff {
			return false
		}
	}
	return true
}

// GGO_METRICS asks GetGlyphOutline for a glyph's metrics without its outline.
const GGO_METRICS = 0

// FIXED is a 16.16 fixed-point value.
type FIXED struct {
	Fract uint16
	Value int16
}

// MAT2 is the transform GetGlyphOutline applies before measuring.
type MAT2 struct {
	EM11 FIXED
	EM12 FIXED
	EM21 FIXED
	EM22 FIXED
}

// GLYPHMETRICS describes a glyph's ink, as opposed to the box it is laid out in.
type GLYPHMETRICS struct {
	GmBlackBoxX     uint32
	GmBlackBoxY     uint32
	GmptGlyphOrigin POINT
	GmCellIncX      int16
	GmCellIncY      int16
}

// GlyphInkHeight returns how tall a character's ink actually is in the font
// currently selected into hdc.
//
// A font's em box is not its ink. Icon fonts leave padding inside the em, so
// asking for a font of height N and expecting an N-pixel glyph comes up short -
// which is the difference between a chevron that matches Windows' and one that
// looks thin and grey beside it.
//
// Reports 0 when the glyph has no outline, which includes whitespace.
func GlyphInkHeight(hdc HDC, r rune) int {
	identity := MAT2{
		EM11: FIXED{Value: 1},
		EM22: FIXED{Value: 1},
	}
	var gm GLYPHMETRICS
	ret, _, _ := procGetGlyphOutline.Call(
		uintptr(hdc),
		uintptr(r),
		GGO_METRICS,
		uintptr(unsafe.Pointer(&gm)),
		0,
		0,
		uintptr(unsafe.Pointer(&identity)),
	)
	if ret == 0xffffffff { // GDI_ERROR
		return 0
	}
	return int(gm.GmBlackBoxY)
}
