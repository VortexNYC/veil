//go:build darwin

package confirm

import (
	"context"
)

/*
#cgo LDFLAGS: -framework CoreFoundation -framework Foundation
#include <CoreFoundation/CoreFoundation.h>
#include <Foundation/Foundation.h>

static void veil_mainloop(void) {
	CFRunLoopRun();
}

static void veil_mainloop_stop(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		CFRunLoopStop(CFRunLoopGetMain());
	});
}
*/
import "C"

// PumpMain parks the calling goroutine's thread — the process main thread —
// in a real CoreFoundation run loop. The bridge daemon needs it: AppKit work
// (the Touch ID access sheet) is dispatched to the main queue from socket
// worker goroutines, and without a run loop nothing drains it.
func PumpMain(ctx context.Context) {
	go func() {
		<-ctx.Done()
		C.veil_mainloop_stop()
	}()
	C.veil_mainloop()
}
