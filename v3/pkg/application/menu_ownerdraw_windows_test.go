//go:build windows && !server

package application

import (
	"testing"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/w32"
)

func TestOwnerDrawMenuDataOwnsAccessibleNativeText(t *testing.T) {
	data, err := newOwnerDrawMenuData("&Open")
	if err != nil {
		t.Fatal(err)
	}
	defer data.release()

	itemData := data.itemData()
	info := (*w32.MSAAMENUINFO)(unsafe.Pointer(itemData))
	if info.DwMSAASignature != w32.MSAA_MENU_SIG {
		t.Fatalf("signature = %#x", info.DwMSAASignature)
	}
	if got := w32.UTF16PtrToString(info.PszWText); got != "&Open" {
		t.Fatalf("text = %q", got)
	}

	if err := data.setText("Save &As"); err != nil {
		t.Fatal(err)
	}
	if data.itemData() != itemData {
		t.Fatal("MSAAMENUINFO address changed during label update")
	}
	if got := w32.UTF16PtrToString(info.PszWText); got != "Save &As" {
		t.Fatalf("updated text = %q", got)
	}
}

func TestMenuItemForUsesStableNativeData(t *testing.T) {
	item := &MenuItem{label: "Item"}
	data, err := newOwnerDrawMenuData(item.label)
	if err != nil {
		t.Fatal(err)
	}
	defer data.release()
	itemData := data.itemData()
	w := &windowsWebviewWindow{menu: &Win32Menu{
		drawMapping: map[uintptr]*ownerDrawMenuEntry{
			itemData: {item: item, data: data},
		},
	}}

	// Menu.Update may replace this mutable platform implementation. Resolution
	// must remain tied to the native menu that receives the draw message.
	item.impl = &windowsMenuItem{ownerDraw: false}
	if got, ok := w.menuItemFor(itemData); !ok || got != item {
		t.Fatal("owner-draw item did not resolve from native item data")
	}
}

func TestMenuMnemonic(t *testing.T) {
	tests := []struct {
		label string
		want  rune
		ok    bool
	}{
		{"&Open", 'O', true},
		{"Save &as", 'A', true},
		{"Rock && Roll", 0, false},
		{"Rock && &Roll", 'R', true},
		{"Trailing&", 0, false},
	}
	for _, test := range tests {
		got, ok := menuMnemonic(test.label)
		if got != test.want || ok != test.ok {
			t.Errorf("menuMnemonic(%q) = (%q, %v), want (%q, %v)", test.label, got, ok, test.want, test.ok)
		}
	}
}

func TestMenuCharResult(t *testing.T) {
	result := menuCharResult(7, w32.MNC_SELECT)
	if index := uint16(result); index != 7 {
		t.Fatalf("index = %d", index)
	}
	if action := uint16(result >> 16); action != w32.MNC_SELECT {
		t.Fatalf("action = %d", action)
	}
}

func TestApplyCustomPopupColours(t *testing.T) {
	background := rgb(24, 16, 48)
	textColour := rgb(244, 240, 255)
	selectedBackground := rgb(0, 109, 119)
	selectedText := rgb(255, 255, 255)
	theme := &w32.MenuBarTheme{
		MenuBarBackground:      &background,
		TitleBarText:           &textColour,
		MenuSelectedBackground: &selectedBackground,
		MenuSelectedText:       &selectedText,
	}

	got := applyCustomPopupColours(darkMenuColours, theme)
	if got.background != background || got.text != textColour {
		t.Fatalf("normal colours = (%#x, %#x)", got.background, got.text)
	}
	if got.selectedBg != selectedBackground || got.selectedText != selectedText {
		t.Fatalf("selected colours = (%#x, %#x)", got.selectedBg, got.selectedText)
	}
	if got.checkBackground != selectedBackground {
		t.Fatalf("check background = %#x", got.checkBackground)
	}
	if want := mix(background, textColour, 50); got.disabledText != want {
		t.Fatalf("disabled text = %#x, want %#x", got.disabledText, want)
	}
	if want := mix(background, textColour, 15); got.separator != want {
		t.Fatalf("separator = %#x, want %#x", got.separator, want)
	}
}

func TestApplyCustomPopupColoursAllowsPartialTheme(t *testing.T) {
	selected := rgb(10, 20, 30)
	base := lightMenuColours
	got := applyCustomPopupColours(base, &w32.MenuBarTheme{MenuSelectedBackground: &selected})

	if got.background != base.background || got.text != base.text {
		t.Fatal("partial theme replaced unspecified normal colours")
	}
	if got.selectedBg != selected || got.checkBackground != selected {
		t.Fatal("partial theme did not apply selected background")
	}
}

func TestApplyCustomPopupColoursLeavesStandardPaletteAlone(t *testing.T) {
	if got := applyCustomPopupColours(darkMenuColours, nil); got != darkMenuColours {
		t.Fatal("nil custom theme changed the standard dark palette")
	}
}
