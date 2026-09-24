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

static int veil_touchid(const char *reason) {
	__block int ok = 0;
	dispatch_semaphore_t sema = dispatch_semaphore_create(0);
	LAContext *ctx = [[LAContext alloc] init];
	NSError *authError = nil;
	if (![ctx canEvaluatePolicy:LAPolicyDeviceOwnerAuthentication error:&authError]) {
		return 0;
	}
	NSString *why = [NSString stringWithUTF8String:reason];
	[ctx evaluatePolicy:LAPolicyDeviceOwnerAuthentication
		localizedReason:why
				  reply:^(BOOL success, NSError *error) {
					ok = success ? 1 : 0;
					dispatch_semaphore_signal(sema);
				  }];
	if (dispatch_semaphore_wait(sema, dispatch_time(DISPATCH_TIME_NOW, 60 * NSEC_PER_SEC)) != 0) {
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

static NSImage *veil_veil_mark(void) {
	const CGFloat s = 128;
	NSImage *img = [[NSImage alloc] initWithSize:NSMakeSize(s, s)];
	[img lockFocus];
	[[NSColor colorWithCalibratedRed:0.07 green:0.12 blue:0.22 alpha:1] setFill];
	[[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(0, 0, s, s)] fill];
	NSColor *stroke = [NSColor colorWithCalibratedRed:0.94 green:0.93 blue:0.91 alpha:1];
	void (^ring)(CGFloat, CGFloat, CGFloat, CGFloat, CGFloat) = ^(CGFloat cx, CGFloat cy, CGFloat rx, CGFloat ry, CGFloat w) {
		NSBezierPath *p = [NSBezierPath bezierPathWithOvalInRect:NSMakeRect(cx - rx, cy - ry, rx * 2, ry * 2)];
		p.lineWidth = w;
		[stroke setStroke];
		[p stroke];
	};
	ring(s * 0.50, s * 0.50, s * 0.42, s * 0.38, 6.0);
	ring(s * 0.54, s * 0.45, s * 0.33, s * 0.30, 4.5);
	ring(s * 0.58, s * 0.39, s * 0.24, s * 0.22, 3.2);
	ring(s * 0.63, s * 0.32, s * 0.16, s * 0.14, 2.2);
	ring(s * 0.68, s * 0.24, s * 0.08, s * 0.07, 1.4);
	[stroke setFill];
	[[NSBezierPath bezierPathWithOvalInRect:NSMakeRect(s * 0.72, s * 0.14, 8, 8)] fill];
	[img unlockFocus];
	return img;
}

@interface PWMAccessSheet : NSObject <NSWindowDelegate>
@property(nonatomic) int result;
@property(nonatomic, copy) NSString *why;
@property(nonatomic, strong) NSWindow *win;
@end

@implementation PWMAccessSheet
- (void)cancel:(id)sender {
	(void)sender;
	self.result = 0;
	[NSApp stopModal];
}
- (void)authorize:(id)sender {
	(void)sender;
	self.result = veil_touchid(self.why.UTF8String) ? 1 : 0;
	[NSApp stopModal];
}
- (BOOL)windowShouldClose:(NSWindow *)sender {
	(void)sender;
	self.result = 0;
	[NSApp stopModal];
	return YES;
}
@end

static NSImageView *veil_icon_view(NSImage *img) {
	NSImageView *v = [[NSImageView alloc] initWithFrame:NSMakeRect(0, 0, 56, 56)];
	v.image = img;
	v.imageScaling = NSImageScaleProportionallyUpOrDown;
	v.wantsLayer = YES;
	v.layer.cornerRadius = 12;
	v.layer.masksToBounds = YES;
	[v.widthAnchor constraintEqualToConstant:56].active = YES;
	[v.heightAnchor constraintEqualToConstant:56].active = YES;
	return v;
}

static int veil_access(const char *action, const char *account, const char *reason) {
	__block int out = 0;
	void (^run)(void) = ^{
		[NSApplication sharedApplication];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		PWMAccessSheet *ctrl = [[PWMAccessSheet alloc] init];
		ctrl.why = [NSString stringWithUTF8String:reason];
		NSRunningApplication *client = veil_client_app();
		NSString *appName = client.localizedName.length ? client.localizedName : @"this app";
		NSImage *clientIcon = client.icon ?: [NSImage imageWithSystemSymbolName:@"terminal" accessibilityDescription:nil];
		NSString *allow = [NSString stringWithFormat:@"Allow %@ to %s", appName, action];
		NSString *who = [NSString stringWithUTF8String:account];

		NSRect frame = NSMakeRect(0, 0, 420, 288);
		NSWindow *win = [[NSWindow alloc] initWithContentRect:frame
			styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable)
			backing:NSBackingStoreBuffered
			defer:NO];
		win.title = @"Veil Access Requested";
		win.level = NSModalPanelWindowLevel;
		win.releasedWhenClosed = NO;
		win.delegate = ctrl;
		ctrl.win = win;

		NSView *content = win.contentView;
		NSImageView *left = veil_icon_view(clientIcon);
		NSImageView *check = [[NSImageView alloc] initWithFrame:NSZeroRect];
		check.image = [NSImage imageWithSystemSymbolName:@"checkmark.circle.fill" accessibilityDescription:nil];
		check.contentTintColor = [NSColor systemGreenColor];
		[check.widthAnchor constraintEqualToConstant:22].active = YES;
		[check.heightAnchor constraintEqualToConstant:22].active = YES;
		NSImageView *right = veil_icon_view(veil_veil_mark());
		NSStackView *icons = [NSStackView stackViewWithViews:@[left, check, right]];
		icons.orientation = NSUserInterfaceLayoutOrientationHorizontal;
		icons.alignment = NSLayoutAttributeCenterY;
		icons.spacing = 16;

		NSTextField *allowLabel = [NSTextField labelWithString:allow];
		allowLabel.font = [NSFont systemFontOfSize:15 weight:NSFontWeightSemibold];
		allowLabel.alignment = NSTextAlignmentCenter;

		NSImageView *acctIcon = [[NSImageView alloc] initWithFrame:NSZeroRect];
		acctIcon.image = veil_veil_mark();
		acctIcon.wantsLayer = YES;
		acctIcon.layer.cornerRadius = 6;
		acctIcon.layer.masksToBounds = YES;
		[acctIcon.widthAnchor constraintEqualToConstant:28].active = YES;
		[acctIcon.heightAnchor constraintEqualToConstant:28].active = YES;
		NSTextField *acctName = [NSTextField labelWithString:who];
		acctName.font = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
		NSImageView *chev = [[NSImageView alloc] initWithFrame:NSZeroRect];
		chev.image = [NSImage imageWithSystemSymbolName:@"chevron.right" accessibilityDescription:nil];
		chev.contentTintColor = [NSColor tertiaryLabelColor];
		[chev.widthAnchor constraintEqualToConstant:12].active = YES;
		[chev.heightAnchor constraintEqualToConstant:12].active = YES;
		NSStackView *acctRow = [NSStackView stackViewWithViews:@[acctIcon, acctName, chev]];
		acctRow.orientation = NSUserInterfaceLayoutOrientationHorizontal;
		acctRow.alignment = NSLayoutAttributeCenterY;
		acctRow.spacing = 10;
		acctRow.edgeInsets = NSEdgeInsetsMake(8, 10, 8, 12);
		acctRow.wantsLayer = YES;
		acctRow.layer.cornerRadius = 8;
		acctRow.layer.backgroundColor = NSColor.controlBackgroundColor.CGColor;
		[acctName setContentHuggingPriority:NSLayoutPriorityDefaultLow forOrientation:NSLayoutConstraintOrientationHorizontal];
		[acctName setContentCompressionResistancePriority:NSLayoutPriorityDefaultLow forOrientation:NSLayoutConstraintOrientationHorizontal];

		NSButton *cancel = [NSButton buttonWithTitle:@"Cancel" target:ctrl action:@selector(cancel:)];
		cancel.keyEquivalent = @"\e";
		NSButton *auth = [NSButton buttonWithTitle:@"Authorize with Touch ID" target:ctrl action:@selector(authorize:)];
		auth.keyEquivalent = @"\r";
		auth.image = [NSImage imageWithSystemSymbolName:@"touchid" accessibilityDescription:nil];
		auth.imagePosition = NSImageRight;
		auth.bezelStyle = NSBezelStyleRounded;
		NSStackView *btns = [NSStackView stackViewWithViews:@[cancel, auth]];
		btns.orientation = NSUserInterfaceLayoutOrientationHorizontal;
		btns.alignment = NSLayoutAttributeCenterY;
		btns.distribution = NSStackViewDistributionEqualSpacing;
		btns.spacing = 12;

		NSStackView *stack = [NSStackView stackViewWithViews:@[icons, allowLabel, acctRow, btns]];
		stack.orientation = NSUserInterfaceLayoutOrientationVertical;
		stack.alignment = NSLayoutAttributeCenterX;
		stack.spacing = 16;
		stack.translatesAutoresizingMaskIntoConstraints = NO;
		[content addSubview:stack];
		[NSLayoutConstraint activateConstraints:@[
			[stack.leadingAnchor constraintEqualToAnchor:content.leadingAnchor constant:24],
			[stack.trailingAnchor constraintEqualToAnchor:content.trailingAnchor constant:-24],
			[stack.topAnchor constraintEqualToAnchor:content.topAnchor constant:20],
			[acctRow.leadingAnchor constraintEqualToAnchor:stack.leadingAnchor],
			[acctRow.trailingAnchor constraintEqualToAnchor:stack.trailingAnchor],
			[btns.leadingAnchor constraintEqualToAnchor:stack.leadingAnchor],
			[btns.trailingAnchor constraintEqualToAnchor:stack.trailingAnchor],
		]];

		[win center];
		[NSApp activateIgnoringOtherApps:YES];
		[win makeKeyAndOrderFront:nil];
		[NSApp runModalForWindow:win];
		[win orderOut:nil];
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		out = ctrl.result;
	};
	run();
	return out;
}

static char *veil_caller_bundle(void) {
	NSRunningApplication *app = veil_client_app();
	if (app.bundleIdentifier.length == 0) {
		return NULL;
	}
	return strdup(app.bundleIdentifier.UTF8String);
}

// First non-shell ancestor process name — the consent key for callers
// with no bundle id (agent CLIs, `go run`, CI). proc_pidpath covers any
// process, not just .app bundles.
static char *veil_caller_label(void) {
	pid_t pid = getppid();
	char buf[PROC_PIDPATHINFO_MAXSIZE];
	for (int i = 0; i < 12 && pid > 1; i++) {
		if (proc_pidpath(pid, buf, sizeof(buf)) > 0) {
			NSString *name = [[NSString stringWithUTF8String:buf] lastPathComponent];
			if (name.length > 0 && !veil_skip_exe(name)) {
				return strdup(name.UTF8String);
			}
		}
		pid = veil_ppid(pid);
	}
	return NULL;
}
*/
import "C"

const touchIDAvailable = true

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
