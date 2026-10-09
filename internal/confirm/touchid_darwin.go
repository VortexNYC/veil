//go:build darwin

package confirm

import (
	"fmt"
	"unsafe"
)

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework LocalAuthentication -framework Foundation -framework AppKit
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/sysctl.h>
#include <libproc.h>
#import <LocalAuthentication/LocalAuthentication.h>
#import <Foundation/Foundation.h>
#import <AppKit/AppKit.h>

static void veil_confirm_log(NSString *msg) {
	struct timeval tv;
	gettimeofday(&tv, NULL);
	struct tm tmv;
	localtime_r(&tv.tv_sec, &tmv);
	fprintf(stderr, "veil-confirm %02d:%02d:%02d.%03d %s\n",
		tmv.tm_hour, tmv.tm_min, tmv.tm_sec, (int)(tv.tv_usec / 1000), msg.UTF8String);
}

static int veil_touchid_ctx(LAContext *ctx, const char *reason) {
	__block int ok = 0;
	dispatch_semaphore_t sema = dispatch_semaphore_create(0);
	NSError *authError = nil;
	if (![ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&authError]) {
		veil_confirm_log([NSString stringWithFormat:@"canEvaluatePolicy=NO err=%@", authError]);
		return 0;
	}
	NSString *why = [NSString stringWithUTF8String:reason];
	veil_confirm_log(@"evaluatePolicy begin");
	[ctx evaluatePolicy:LAPolicyDeviceOwnerAuthentication
		localizedReason:why
				  reply:^(BOOL success, NSError *error) {
					veil_confirm_log([NSString stringWithFormat:@"evaluatePolicy reply ok=%d err=%@", success, error]);
					ok = success ? 1 : 0;
					dispatch_semaphore_signal(sema);
				  }];
	if (dispatch_semaphore_wait(sema, dispatch_time(DISPATCH_TIME_NOW, 60 * NSEC_PER_SEC)) != 0) {
		veil_confirm_log(@"evaluatePolicy timeout 60s");
		[ctx invalidate];
		return 0;
	}
	return ok;
}

static pid_t veil_ppid(pid_t pid) {
	struct kinfo_proc kp;
	size_t len = sizeof(kp);
	memset(&kp, 0, sizeof(kp));
	int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PID, pid};
	if (sysctl(mib, 4, &kp, &len, NULL, 0) != 0 || len == 0) {
		return 0;
	}
	return kp.kp_eproc.e_ppid;
}

static BOOL veil_skip_exe(NSString *exe) {
	return [exe isEqualToString:@"veil"] ||
		[exe isEqualToString:@"native-host"] ||
		[exe isEqualToString:@"zsh"] ||
		[exe isEqualToString:@"bash"] ||
		[exe isEqualToString:@"sh"] ||
		[exe isEqualToString:@"fish"] ||
		[exe isEqualToString:@"nu"] ||
		[exe isEqualToString:@"login"] ||
		[exe isEqualToString:@"sshd"];
}

static NSRunningApplication *veil_client_app(void) {
	pid_t pid = getppid();
	for (int i = 0; i < 12 && pid > 1; i++) {
		NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
		NSString *exe = app.executableURL.lastPathComponent ?: @"";
		if (app.bundleIdentifier.length > 0 && !veil_skip_exe(exe)) {
			return app;
		}
		pid = veil_ppid(pid);
	}
	return nil;
}


static void veil_warm_app(void) {
	// The --confirm-server child runs this at spawn: every per-request
	// veil_access then skips the 1.5s LaunchServices handshake +
	// sharedApplication boot + the ~1.3s first window build.
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSProcessInfo processInfo].automaticTerminationSupportEnabled = NO;
	[NSApp finishLaunching];
	veil_confirm_log(@"warm appkit done");
}

static int veil_access(const char *action, const char *account, const char *reason) {
	__block int out = 0;
	void (^run)(void) = ^{
		veil_confirm_log(@"eval run enter");
		[NSApplication sharedApplication];
		veil_confirm_log(@"sharedApplication done");
		// Prohibited keeps the helper off the Dock — the LA dialog is
		// presented by the system on our behalf, not by app chrome.
		[NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
		veil_confirm_log(@"activationPolicy done");
		// finishLaunching makes this plist-less helper a managed app —
		// without the opt-out, efficiency termination can kill it mid-eval
		// and the daemon's socket reply never lands. The flag (not the
		// counter) sticks because AppKit unbalances the counter as windows
		// open and close.
		[NSProcessInfo processInfo].automaticTerminationSupportEnabled = NO;
		[NSApp finishLaunching];
		veil_confirm_log(@"finishLaunching done");
		NSRunningApplication *client = veil_client_app();
		NSString *appName = client.localizedName;
		NSString *what = [NSString stringWithUTF8String:action];
		NSString *who = [NSString stringWithUTF8String:account];
		// Exactly one surface: the macOS LA dialog. No Veil window behind
		// it — stacked prompts (ours, then the system's, then Touch ID)
		// read as three asks for one approval. The reason reads as a
		// continuation of "Veil is trying to …": action, the calling app
		// when one resolves, and the vault account.
		NSString *why = what;
		// "for Veil" adds nothing when Veil itself is the asking app.
		if (appName.length && ![appName isEqualToString:@"Veil"]) {
			why = [NSString stringWithFormat:@"%@ for %@", why, appName];
		}
		if (who.length) {
			why = [NSString stringWithFormat:@"%@ — %@", why, who];
		}
		LAContext *ctx = [[LAContext alloc] init];
		out = veil_touchid_ctx(ctx, why.UTF8String);
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
	};
	// Callers off the main thread — bridge socket workers, CLI helpers —
	// cannot run AppKit where they stand. Queue the modal onto the main
	// queue and wait; the main thread owns a real run loop everywhere.
	if ([NSThread isMainThread]) {
		run();
	} else {
		dispatch_semaphore_t sema = dispatch_semaphore_create(0);
		dispatch_async(dispatch_get_main_queue(), ^{
			run();
			dispatch_semaphore_signal(sema);
		});
		dispatch_semaphore_wait(sema, DISPATCH_TIME_FOREVER);
	}
	return out;
}

static char *veil_caller_bundle(void) {
	NSRunningApplication *app = veil_client_app();
	if (app.bundleIdentifier.length == 0) {
		return NULL;
	}
	return strdup(app.bundleIdentifier.UTF8String);
}

// First non-shell ancestor process path — the consent key for callers
// with no bundle id (agent CLIs, `go run`, CI). proc_pidpath covers any
// process, not just .app bundles, and the FULL PATH is the key: a stray
// binary that happens to share the name does not inherit the consent.
static char *veil_caller_label(void) {
	pid_t pid = getppid();
	char buf[PROC_PIDPATHINFO_MAXSIZE];
	for (int i = 0; i < 12 && pid > 1; i++) {
		if (proc_pidpath(pid, buf, sizeof(buf)) > 0) {
			NSString *name = [[NSString stringWithUTF8String:buf] lastPathComponent];
			if (name.length > 0 && !veil_skip_exe(name)) {
				return strdup(buf);
			}
		}
		pid = veil_ppid(pid);
	}
	return NULL;
}
*/
import "C"

const touchIDAvailable = true

// warmAppKit runs NSApp init once at --confirm-server spawn so the first
// request never pays it. Must run on the main thread (ServeStdio is).
func warmAppKit() {
	C.veil_warm_app()
}

// TouchID is the Veil access sheet, then device owner auth. Cancel fails closed.
func TouchID(reason string) error {
	if reason == "" {
		reason = "Veil wants to fill a saved sign-in"
	}
	action := Action(reason)
	account := accountLabel()
	ca := C.CString(action)
	cc := C.CString(account)
	cr := C.CString(reason)
	defer C.free(unsafe.Pointer(ca))
	defer C.free(unsafe.Pointer(cc))
	defer C.free(unsafe.Pointer(cr))
	if C.veil_access(ca, cc, cr) != 1 {
		return fmt.Errorf("fill: touch id declined")
	}
	return nil
}

func callerBundle() string {
	p := C.veil_caller_bundle()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

func callerLabel() string {
	p := C.veil_caller_label()
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}
