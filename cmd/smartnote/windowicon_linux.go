//go:build linux && cgo && !gtk3

package main

/*
#cgo pkg-config: gtk4
#include <stdlib.h>
#include <gtk/gtk.h>

// smartnote_use_icon makes the icons in dir known to GTK and makes name the
// icon of every window: GTK 4 takes window icons from the icon theme only.
// The default name reaches only windows realized later, so the windows that
// already exist get the name too.
static void smartnote_use_icon(const char *dir, const char *name) {
	GdkDisplay *display = gdk_display_get_default();
	if (display != NULL) {
		gtk_icon_theme_add_search_path(gtk_icon_theme_get_for_display(display), dir);
	}
	gtk_window_set_default_icon_name(name);

	GList *windows = gtk_window_list_toplevels();
	for (GList *l = windows; l != NULL; l = l->next) {
		gtk_window_set_icon_name(GTK_WINDOW(l->data), name);
	}
	g_list_free(windows);
}
*/
import "C"

import (
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// useWindowIcon shows the logo in the title bar. GTK 4 has no window icon
// from bytes, so the logo is written as an icon theme to the user's cache
// directory and used by its name; an installed package provides the same
// name in the system hicolor theme.
// Call it on the running application.
func useWindowIcon() error {
	icons, err := installIconTheme()
	if err != nil {
		return err
	}

	dir := C.CString(icons)
	defer C.free(unsafe.Pointer(dir))

	name := C.CString(iconName)
	defer C.free(unsafe.Pointer(name))

	application.InvokeSync(func() { C.smartnote_use_icon(dir, name) })

	return nil
}
