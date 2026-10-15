//go:build darwin

package app

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#import <objc/runtime.h>

static NSStatusItem *lhStatusItem = nil;
static id lhTrayDelegate = nil;

extern void lhTrayActionOpenApp(void);
extern void lhTrayActionOpenSettings(void);
extern void lhTrayActionQuit(void);

static void lh_open_app_impl(id self, SEL _cmd, id sender) {
    lhTrayActionOpenApp();
}

static void lh_open_settings_impl(id self, SEL _cmd, id sender) {
    lhTrayActionOpenSettings();
}

static void lh_quit_app_impl(id self, SEL _cmd, id sender) {
    lhTrayActionQuit();
}

static id lh_get_or_create_delegate(void) {
    Class cls = objc_getClass("LHTrayDelegate");
    if (cls == Nil) {
        cls = objc_allocateClassPair((Class)objc_getClass("NSObject"), "LHTrayDelegate", 0);
        class_addMethod(cls, sel_registerName("openApp:"), (IMP)lh_open_app_impl, "v@:@");
        class_addMethod(cls, sel_registerName("openSettings:"), (IMP)lh_open_settings_impl, "v@:@");
        class_addMethod(cls, sel_registerName("quitApp:"), (IMP)lh_quit_app_impl, "v@:@");
        objc_registerClassPair(cls);
    }
    return [[cls alloc] init];
}

static void lh_init_tray(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (lhStatusItem != nil) {
            return;
        }
        lhStatusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
        [lhStatusItem retain];
        if (lhStatusItem.button != nil) {
            lhStatusItem.button.title = @"⚓";
            lhStatusItem.button.toolTip = @"Lum Harbor";
        }
        lhTrayDelegate = lh_get_or_create_delegate();

        NSMenu *menu = [[NSMenu alloc] init];

        NSMenuItem *statusRow = [[NSMenuItem alloc] initWithTitle:@"Lum Harbor is running" action:nil keyEquivalent:@""];
        [statusRow setEnabled:NO];
        [menu addItem:statusRow];
        [statusRow release];

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *openItem = [[NSMenuItem alloc] initWithTitle:@"Open Lum Harbor" action:@selector(openApp:) keyEquivalent:@""];
        [openItem setTarget:lhTrayDelegate];
        [menu addItem:openItem];
        [openItem release];

        NSMenuItem *settingsItem = [[NSMenuItem alloc] initWithTitle:@"Settings" action:@selector(openSettings:) keyEquivalent:@""];
        [settingsItem setTarget:lhTrayDelegate];
        [menu addItem:settingsItem];
        [settingsItem release];

        [menu addItem:[NSMenuItem separatorItem]];

        NSMenuItem *quitItem = [[NSMenuItem alloc] initWithTitle:@"Quit Lum Harbor" action:@selector(quitApp:) keyEquivalent:@"q"];
        [quitItem setTarget:lhTrayDelegate];
        [menu addItem:quitItem];
        [quitItem release];

        lhStatusItem.menu = menu;
        [menu release];
    });
}

static void lh_set_hide_on_close(bool shouldHide) {
    dispatch_async(dispatch_get_main_queue(), ^{
        for (NSWindow *window in [NSApp windows]) {
            id delegate = [window delegate];
            if (delegate && [delegate respondsToSelector:@selector(setHideOnClose:)]) {
                @try {
                    [delegate setValue:[NSNumber numberWithBool:shouldHide] forKey:@"hideOnClose"];
                } @catch (NSException *e) {
                }
            }
        }
    });
}
*/
import "C"

import (
	"sync"
)

var (
	trayAppMu sync.Mutex
	trayApp   *App
)

func initTray(a *App) {
	trayAppMu.Lock()
	trayApp = a
	trayAppMu.Unlock()
	C.lh_init_tray()
}

func setNativeHideOnClose(shouldHide bool) {
	C.lh_set_hide_on_close(C.bool(shouldHide))
}

//export lhTrayActionOpenApp
func lhTrayActionOpenApp() {
	trayAppMu.Lock()
	a := trayApp
	trayAppMu.Unlock()
	if a != nil {
		a.ShowMainWindow()
	}
}

//export lhTrayActionOpenSettings
func lhTrayActionOpenSettings() {
	trayAppMu.Lock()
	a := trayApp
	trayAppMu.Unlock()
	if a != nil {
		a.OpenSettingsFromTray()
	}
}

//export lhTrayActionQuit
func lhTrayActionQuit() {
	trayAppMu.Lock()
	a := trayApp
	trayAppMu.Unlock()
	if a != nil {
		a.QuitFromTray()
	}
}
