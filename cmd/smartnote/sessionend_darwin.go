//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreServices
#import <Foundation/Foundation.h>
#import <CoreServices/CoreServices.h>

// quitForSessionEnd reports whether the Apple event being handled is the quit
// that logout, restart or shutdown sends; Cmd+Q and the Dock send none or one
// without such a reason.
static int quitForSessionEnd(void) {
	@autoreleasepool {
		NSAppleEventDescriptor *event = [[NSAppleEventManager sharedAppleEventManager] currentAppleEvent];
		if (event == nil || [event eventClass] != kCoreEventClass || [event eventID] != kAEQuitApplication) {
			return 0;
		}
		switch ([[event attributeDescriptorForKeyword:kAEQuitReason] enumCodeValue]) {
		case kAELogOut:
		case kAEReallyLogOut:
		case kAEShowRestartDialog:
		case kAERestart:
		case kAEShowShutdownDialog:
		case kAEShutDown:
			return 1;
		default:
			return 0;
		}
	}
}
*/
import "C"

// sessionEnding reports whether macOS asks the app to quit for logout,
// restart or shutdown. Wails calls ShouldQuit from applicationShouldTerminate
// while the quit event is being handled, so the event is still current.
func sessionEnding() bool { return C.quitForSessionEnd() != 0 }
