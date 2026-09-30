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

static NSImage *veil_veil_mark(void) {
	static NSImage *img = nil;
	if (!img) {
		NSData *png = [[NSData alloc] initWithBase64EncodedString:
		@"iVBORw0KGgoAAAANSUhEUgAAAQAAAAEACAYAAABccqhmAAAABmJLR0QA/wD/AP+gvaeTAAAfVUlE"
		@"QVR4nO3dd2BUVb4H8O+5M8mkkGQmM6GYhOLSJEFQwOATpCgiiAgIgijYsFEsa30+KaL73q4rWLGh"
		@"i4r7FHEfYgF0FVRAFwGxABJ0VxICpExJQkgyk8yc9weEDZjAlHPvmTnz+/wDSWZ+50e45ze3nMIQ"
		@"o9q2TW0X8CX0hMa7A6wHGLqDIwNAKgDbsT/bHPuTkHAdAVBz7E8PGGoAVIOzQiCwFwG2V0ts2FNe"
		@"fqRMbprhYbITCJK5rc1WwDU+jHMMA9APQIbspAhpphLAdsbYBsbZhnK3+xsAjbKTOp2oLQAOh6MD"
		@"Ao1TAH4JwAbh6Kc5IbGiBuBfMrBPYEpYUVFRUSo7oZZEVQHo3BlJR6ozLueMTQfHpQDMsnMiRIAA"
		@"gPWcYbnZbPlbWVnZEdkJNYmKAmC323sy7r8XwCQA6bLzIURH1QBbAa1xkdN5uFB2MlILQFub7ewA"
		@"+L1gmArAJDMXQgwWAMMaMP6I01m1TVYSUgpAZmbmQA3+uQAbJSsHQqIEB8OHHIFHXa7qrUY3bmjn"
		@"S09Pz0wwa/MZMBuAZmTbhEQ5Do43TYmN95aV1ZQb1ahRp92a3W6dZtbYBwwYBvrUJ+RkDAx9eECb"
		@"kZqcVF9bV78NANe/UZ21tdnODjD+KoD+erdFiDrYFj/HDI/Hs1PPVnQ9A7DbrdMBrALQSc92CFFQ"
		@"jsb4janJyTW1dfVb9GpElzMAh8ORBt7wIjibqkd8QuIKw5tMS7i9oqKiRnxowdparX0CGlYC6CY6"
		@"NiHxi+8xce2qMo/nR5FRhRYAuz1jOONsFWgwDyF6qAmAX+l2V30iKqCwR3F2u3U84/gI1PkJ0Usb"
		@"DeyDrMyMyaICCrkJaLfbZjGOZQBLEBGPENIqE8CuTElOOlxbV/+1gGCRcWRa5zHgz6Bn+4QYhQEY"
		@"mZqS3FhbV78xkkARFQC7zTaTMfw5khiEkLANT01OqozkMWHYBcBhs01hjL8C+uQnRKZLUpMte2rr"
		@"vLvCeXNYndduzxjOwNaAwxLO+wkhQvkC4JeH83Qg5AJw7Dn/JtAKPYREk2oTZ4NCHScQUgHIyspq"
		@"w/2+rQDrGVpuhBAD/AzN3M/pdB4O9g0hjQPggYYXqPMTErW6IdDwcihvCPomoMOWcSvAHgo9J0KI"
		@"cVh+anLS/tq6+h1BvTqYFx2d0hvYArCkyJIjhBig1s9ZQTBTiYO5BNACDC9T5yckZqSYGP8Lgujf"
		@"p32B3Wa7DeAFQtIihBhlgCMz46bTveiUlwBpaWn2pARTIQfs4vIihBjEbfY19iytqalo7QWnPANI"
		@"SjAtos5PSMzKbEw0//epXtDqGcDRpbsDX53qNYSQqBfgLDCwtSXHW916S4N/HsCo8ysmNTUVVmsG"
		@"0jMykN4mDQBQdbgah6ur4fFUora2VnKGRDCNBbSHAVzR0g9b7OBZVmtfruHb1n5OYoPJZMKA/v3R"
		@"v38/5OX3Ql6vPJxxRodTvufgwUPYtXsXdu3cja1bt2Hb9u3w+/0GZUx0wqHx/k5n1bcn/6DFDm7P"
		@"tL7LgCv1z4uIxhjDwIICXHbZKFwyYgQy7ZkRxXO5Xfjk47/jo4/WYss334Bz3ZeqJ7pgK5xuz5Tf"
		@"fPfkbxzbqHMXaOeemKJpGoYOG4I5s2ehd36+Lm0U7inEK68uwwcffojGxkZd2iC68UPz5528Ielv"
		@"CoAj07oUwAzD0iIRGzlyJB647x507NTRkPaKi4rxp8efwMefCFubkhiCvex0e2494TvNv8jJyUmu"
		@"r605BCDD0LxIWHJyc7Bg3jwMHXqhlPY3bPgCCxYuxIGSA1LaJyGrTkpp076kpKSu6RsnTAZKNJkm"
		@"H9uqm0S5qVOn4OWXXkDXrr+TlkOXLp1x9ZTJcLnd2LUrrAVpiLEs/gbvj81XDzqhACSnJP+ZAV2N"
		@"z4sEy5KYiMcefQSzZ82E2dzqU1zDmM1mXDR8GHI75WLjl5vo3kDUY8m1dfX/e/yrpr84HI4zEGgs"
		@"hnE7BpMQtW/fDi+/+AJ65fWSnUqLdu7ahVtuuR3l5Ybtbk1C18hMCbkVFRWlQPM7/YHGyaDOH7Wy"
		@"s8/A/765PGo7PwDk5+Vh5Yq3kNsxV3YqpHVmBBomNX3R7FEfv0RGNuT0zuzSBSvefsuwu/yRyM7J"
		@"xl+Xv4HOnTrLToW0ggMjmv7edAlgdmRa3QDS5KREWpOdfQZWrlyBtllZslMJSXl5Oa6ceBUOHSqV"
		@"nQr5rcNOd2UmgEYNANrabAWgzh91UlNT8fKLL8Rc5weAtm3b4i+vvoK0NDqsolCa3Z7eDzh2CcA1"
		@"PkxuPuRkJpMJTy5ehB49e8hOJWzdunXF008tjoqnFeRkpuHAsZt+KclJ8wF0kZoPOcH9992LiRMn"
		@"yE4jYp06dUJiYiI2f/WV7FRIMwzgtXX1y5tuAp4rNRtygvMKBuCmm26QnYYwM2bciPMHDpSdBjlR"
		@"PwBgWVlZ7bm/4ZDsbMhR6enp+PCD1aedthtrysrKcNmYsaisrJKdCjnG7Gtsq/GGhti9yFTQwkcW"
		@"KNf5AaBdu3aYN3eu7DRIM/6EhB4aNE4FIEoMLCjAmDGjZaehm7Fjx6DgvPNkp0GasEAPDWBUAKKA"
		@"yWTC3IfV33hpwYJ59FQgSnBoPTRwmvwTDaZOmRzTj/yC1a1bV1x11UTZaRAA4Lwbc2RavwQwWHYu"
		@"8cxiseCLLz6Dw+7Qva1du3dj45ebsXPXTjhdTgCAw+5Afl4+Lhx8gSFzDSqcTgwdMhxen0/3tkjr"
		@"GPjXZtAIQOmumnil7p3/759+iueXvIgfd7a8Xdy6dR/jiUWL0OfsszFr5u0YfpF+Y8OyHA5MnHQl"
		@"/vrXt3Rrg5weB7Mzh836MxhdBsiSkJCATz9Zh+ycbF3iu9wuPPSfc/HZ+vUhvW/ExRfjD48tjHhR"
		@"0dbsL96PESNH0foBMjFUaGBIl51HPBszerRunX/PT3swYcKkkDs/cPSMYfzESdhbuFeHzIDcjrkY"
		@"PfpSXWKTIHEkaQDayM4jnl099TcrNQtRuKcQ106/DgcOHAw7xoGSA5gy9Vrs2r1bYGb/dvVkff7t"
		@"JGgWDUCy7CziVZfOXXDOOX2Fxy0uKsa0628QMuquuroaN95wM0r2lwjI7ET9B/RDp46dhMclQUvU"
		@"QLv/SDPhynFggndf8/q8mHPHXXC73MJiutwu3DpzFurr64XFBI5uYjJu/FihMUloaPMPSTRNw7gr"
		@"WtyuLSJ/+MP/6HLKXrinEH/64+PC404YPx6aRoehLPSbl6R3fj46dGgvNOa3O77D22+/IzRmc399"
		@"621s37ZdaMzs7DPQ66yzhMYkwaMCIMmFF4ode+X3+/Hww3MRCASExm0uEAhg3iMLhbcxZIicjU0I"
		@"FQBphgjezWf16vexd+/PQmO2pHBPIVZ/8IHQmKKLIQkeFQAJbDar0A08Gxsb8cxzS4TFO51nn10i"
		@"dMvwvn37ICODhqPIQAVAgv79+sNkErcFw6effqbLY7rWFBcVhzW4qDUmkwn9+/UXFo8EjwqABPm9"
		@"84TGe+2NN4TGC6rN18W2Kfp3QoJDBUACkaf/xUXF2L7tW2HxgvXNlq0oKi4SFi8/T9zvhASPCoAE"
		@"efniptyu/uADcM6FxQvFRx+tFRar99l0BiADFQCDtW3bFvZMu7B4a9euExYrVGsEtu2wO5Dl0H89"
		@"BHIiKgAGy83NERartLTMkEd/rSncU4iysjJh8XJ0mhVJWkcFwGAiV/zduHGjsFjh4Jxj48ZNwuKd"
		@"kU0FwGhUAAyWnX2GsFjfbNkqLFbYOXwjLgcVl0OPdlQADNahvbgC8N33PwiLFa4ffhCXQ4czxP1u"
		@"SHCoABjMLmiJrerqauwr2ickViT+9es+1NTUCImVZRd3c5QEhwqAwZKTU4TE+fnnX6Q9/msuEAjg"
		@"l3/+U0gsiyVJSBwSPCoABktOEnOQ79u3T0gcEX799VchcVJSaXEqo1EBMJgl2SIkzr594kbhRerX"
		@"f+0TEic5mQqA0agAGCw5ScxBLvL5e6RKBeUi6ndDgkcFwGCaoFmAFU6XkDgiuN1ichH1uyHBowIQ"
		@"o1yCOp0IzoroyYWEhgpAjKqtqZWdwnF1ddGTCwkNFYAY5Wvwyk7huHra5DNmUQGIUT5vg+wUjvPW"
		@"RU8xIqGhAhCj/FzcmnyRiqZcSGioABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gA"
		@"EBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABAS"
		@"x6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseo"
		@"ABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQEseoABASx6gAEBLHqAAQ"
		@"EseYI9PKZScRC5KSktC5S2d0aN8ONqsNmXY7UlKSYUm0hBSnpqYGAR6IOJ+0NulgLOIwQnAOHK6p"
		@"jjiOxjS0adMmpPd4fV4cOVILj9sNT6UHh0rLsO/Xfaivr484n3hglp1ANDKZTOjbtw/O698ffc/t"
		@"i7xeeWjfvp2Q2JMmTQbTNCQnJYcdg3OOe+/7PXrn54NFQRXwuD24/IpxyO2Yi0RzYthxrDYrHnzg"
		@"fnTo0D7inA4dKsXun3Zjx44d2Lp1O7777nv4/f6I46qGzgCaGXBef0yaOBHDhgyBLdMmNHZZWRn+"
		@"+KfHMWfObJzZpUvE8QYUnI+UlBTcdeccjB83TkCG4fn+hx/w/JIX8dJLz0cc65d//guXXjoa557T"
		@"Fw8++ADOPfccARke5XF7sOHzz/HOu+9i29btwuLGurgvAJqm4bLLRmHm7bejW7euurSxr2gfpk6d"
		@"jvLycqxbtwZdf3dmxDEHFJwPj9sDALjhhuvxXw89GHHMUH362We44867kZqaiq1bvo44XlMBAICE"
		@"hAQseuJxjB49KuK4J9tbuBdLXngRa9asBedxffjH903As3v3xvvv/R+eXLxIt87v9/sx5467UV5e"
		@"rkt8AFi27DWsXbtOt/gtKS0tw9133wuf16dL/IaGBtx3/4Mo2V8iPHb3Ht3x9FOLsXrV39A7P194"
		@"/FgSl/cAGGOYM3sWZs68DWazvr+Cz7/4Aj/t/un41++8sxL2zMyI49bXnXiT67nnX8CoUZdGHDdY"
		@"y157HXV1dcdzeemlpRHHdLndJ3zt9Xrx6qt/wfwF8yKO3ZJeeb2wcuXbWLLkBTy35Pm4PBuIu0sA"
		@"i8WCRU88jksvHWlIewsXPoY3lr9pSFtbtmyGPdNuSFtjxo7Dnp/26N7OmV264JNP1urezpo1a3Hf"
		@"fQ/A69PnjCZaxdUlQEJCApY896xhnR8A3B736V8kiMtpXFtulzFtudwuQ9oZPXoUnn3mad3PCKNN"
		@"XBWAhY/Mx9ChFxrapt3uMKwth92YT38AcGQZ8+/Kchj3+xt+0TAsmD/XsPaiQdwUgLFjx2DSpImG"
		@"tzt40CBD2snPy0OmPfJ7C8G6cPAFhrQzaPBgQ9ppMmXKZF2ePESruCgAVmsG5s/T50bS6QwZMhh9"
		@"+/TRvZ075szWvY3mrrvuOqSlpenaRkpKCm668QZd22jJIwvmIz093fB2ZYiLAjDz9tuQkSHnP5Qx"
		@"hqeffhLZOdm6tTF71kwMv2iYbvFbkuVw4JmnFyMpKUmX+JbERDy1eJGQUYGhstmsmHn7rYa3K4Py"
		@"TwEyMtKxedOXuh2owXK73Hjy6Wfw3nurjz8+i1T3Ht1x151zcMmIEULihWNv4V48/sQT2Lhxs5Ch"
		@"tiaTCf9x/vm4/757cFavswRkGJ66ujpcMGgIqqsjn+MQzZQvALJGybXG6/OiaF8xnC5n2M+dkyxJ"
		@"yM7OFjY/QYSqqmoU7y+OqMOkpaWhY25HWK0ZAjML36OP/gGvv7Fcdhq6Ur4ArFr1btyP9iLh2fHd"
		@"95g0abLsNHSl9D0Ae6Ydeb16yU6DxKg+Z/c2bGCVLEoXgIKB50HTlP4nEh1pmob+A/rLTkNXSvcO"
		@"+vQnkcrLk3cj0ghKF4AePXrIToHEuJ7d1T6GlC4AHaLoLjmJTTLGIRhJ6QKQlZUlOwUS49q1U/tD"
		@"RNkCYDKZYLVZZadBYpzVZoXJZJKdhm6ULQDpGen0BIBETNM0pKfrO+dBJmV7iDWdPv2JGBkKH0vq"
		@"FgBrfMzmIvqLlqHJelC3AND1PxHEaqMCEHPoEoCIYrWqeywpWwDSFT5tI8bKSFf3WFJ2BURZC4Do"
		@"hXOOvYV7ceDAQVRWVcJmy0TnLh3RpXPkuwxF4l+//oqifcXweNywZliRk5ONbt27RcWWZaJkKPxh"
		@"omwBUOW0raqqGktfWYpVq95HWVnZb37eqWMnXHXVRFx//XRYLKFtVBqu+vp6vPbacqxc+S6Kiot+"
		@"8/P27dth/BVXYMbNM5QoxKocSy1R9hJAhdO29Z9twPCLLsaLLy5tsfMDQFFxEf78xCJcPGIkdnz3"
		@"ve45fbvjO1w8YiSeWLSoxc4PHN016IWXXsZFF4/A+vWf656T3lQ4llqjbAGwxfhTgLfeehu3zZyF"
		@"qqrgVtg5dKgU114zTdcOt/6zDbh22nSUlrZcjE5WWVmF226fiRUr3tEtJyPE+rF0KsoWgFi+btv8"
		@"1Vd4ZOFjCAQCIb3P6/Ph7t/fg59//kV4TnsL9+Ku398T8l6AgUAA8xcsxNf/+IfwnIwSy8fS6Shb"
		@"AGwZsVm1fb4GzJ23AI2NjWG9/8iRI5g7f77grIC58xegtrY2rPc2NjbiPx96GD5fg+CsjEEDgWJQ"
		@"ekZs/qetWrUKxUXFEcXYtnU7Nm3eLCgjYOPGjdi+/duIYpTsL8Hq1asFZWQsGgocY0wmU8xO4Pjo"
		@"IzEbYa5d+7GQOADw0RoxW4+vERTHaOnpacrOCFSyAKSnp8XkTMBAIIBt27cJifWPr7cIiQMA32zd"
		@"KiTOlq3fhHxfIxqoPCMw9npJEKwZNtkphMXldgu7Ti4tKxUSh3OOskNiYvm8PngqPUJiGU3VywAl"
		@"C0CsDj6prj4sLJbX64XX6404js/ng9cX2p3/U6msjM2ddlS9EahkAbBlxma1DnenID3jRWNOMqg6"
		@"I1DJAkAzAYloqg4HVrIA0ExAIpqqw4GVLACqXq8ReVQdDahkAchQ9HSNyEOXADHEqujpGpGHLgFi"
		@"iE3RO7ZEHlVnBCpZAOgSgIhG9wBiSKwOBEoWuKKPyWSC2Rz5gk9ms1nosGqR/0YjqXpjWckCEKtD"
		@"gbOysoStpWe324UVAHtmpoCMAMZYzO7XSEOBY4TJZEJaWhvZaYQl0ZKI350pZpHPXr16CokDAGf1"
		@"OktInK5df4dES6KQWEZTdUagcgUgVmcCNrl4xEVi4lwkJo7IWCJzMpqqMwJjt6e0IlZP/5tMu/Za"
		@"JCUlRRTDnmnH5ZePEZQRMG7cWGQ5HBHFsFgsmDr1akEZyaHiZYByBSBWbwA2adeuHW6ecVNEMR54"
		@"8D6kpqYKyghISUnBPffcHVGM2267BR06tBeUkRwq3ghUrwAosCno7NkzMXTohWG995prrsaE8eME"
		@"ZwRMnHglrr56SljvHT58KGbNvF1oPjKocGydTLkCkGkVc8daJpPJhOefew7jx4XWka+7bjrmzX1Y"
		@"p6yABfPn4tZbbw7pPWPGjMazzzwd0/dlmthssX152ZLY/185SXqMXwI0SbQk4vHH/wdPLl6EnNyc"
		@"U76251k9sWzZK5j78EO63qk2mUy47957sOwvS9GjZ49Tvja3Yy6eenIxnnpysWE7FulNxeHAym0N"
		@"ptKILcYYLr/8MowaNRLbtm/Hl19uREnJAVRVVsHuyERuTi6GXzQMZ/fubehefIMHD8agQYPw/Q8/"
		@"YMP6z7G/ZD9cTjcyrBnIycnGkCEXon+/fso9NlPp2GqiXAGwKniaZjabMbCgAAMLCmSnchxjDH37"
		@"9EHfPn1kp2IYFWcEKncJQDMBiV5UvARQrgDQTECiFxVnBCpXAGgmINGLivcAlCsAdAlA9EIDgWIA"
		@"nQEQvdBQ4CgXyzMBSfRTcUagUgUgPSNdiRFnJDqpOCNQqd5CG4IQval2GaBWAVBwsgaJLqrdCFSr"
		@"ACj4nJZEF9X2CFSrACh2ekaij2rDgZUqALQnINGbasOBlSoAql2fkeij2mhApQoADQIieqNLgChG"
		@"w4CJ3ugSIIrRTECiN9VmBGoAuOwkRElLp3EARF+qLDnXRANQJzsJUVKSkmWnQBSXlKjG+obH+DQA"
		@"NbKzEMWcmCA7BaI4c2Jsbm3WCq8GjmrZWYhi1tSaqUWij8lk3OKrumOo18DUOQOor6+XnQJRXN0R"
		@"Za6YAc7cGoDDsvMQpaa2VnYKRHG1Sh1j3KmBwy07DVEq3R7ZKRDFeaoqZacgDkeFBoZfZOchSsnB"
		@"A7JTIIor2V8iOwVxNLZXA3ih7DxEKSoqlp0CUdz+4v2yUxCGcV6osQBTpgDs3LlTdgpEcT8qdIwx"
		@"zgo1ltigUAHYhYaGBtlpEEX5fA3Yvfsn2WkIY25sLNTKy4+UAVDizobX68XWbdtkp0EUtWXLP+D1"
		@"+WSnIYr74OHDzqbJQNulpiLQ3z/+u+wUiKLWKXRsMbBtwLHZgIyxDXLTEefDtWvh9Xplp0EUU1dX"
		@"h3Xr1slOQ5gAxwagqQBwdQqAx+3BqlXvyU6DKObd/1uFqiplRs0Dmn89ADQNbDY7Mq1uAErsepCT"
		@"m4OP130Ei1ozt4gk9fX1uGTkKBw8eEh2KqJUOd2VdgD+pnsAjQDfJDMjkUr2l2Dp0ldlp0EU8dJL"
		@"S1Xq/ADHFwD8QLMVgRjYx9IS0sHzL7yInbt2yU6DxLgfd+7ESy+/LDsNobjGjt/N/HcBMDe8DaBR"
		@"SkY68Hl9uOPOu+HxKPGEk0jgcXsw54674PMpNbak0WTyrWz64ngBODoegKvznANAcVExpl9/g1o3"
		@"b4ghDh8+jBtunKHW2H8AYFh7bOwPgJMXBeXacsMT0tlPu3/CtOuux6FDpbJTITHi4MFDuPqaaUpe"
		@"QjLOT+jjJxSANhmeVQCqDM3IALt37caECROx+auvZKdCotzGTZswYcIk7Plpj+xU9FBlSUn7sPk3"
		@"TlhDq7ISjSnJyWcC6GdoWgaora3F6tXv49ChUpzbty+SU2gBUfJvLrcLjzzyGP74x8cVW/SjOfZ6"
		@"aVn5CYNkfrPAWVZWelfu1/bgpOKgkpSUFFw3fRquu34aHHaH7HSIRBVOJ9547Q28vvxNhTs+AMAP"
		@"zd/L6Ty8t/k3W1zh0JFpWwHwq4zJS56EhAQMHz4c48ePxaALLkBSUpLslIgB6urqsGnzZry36n2s"
		@"37AhTmaQ8rec7qqpJ3+3xQLQ1mrtE9Cwo7Wfq8iSmIiCgQU499xz0LdvH+T1ylNuF5h45XF7sHP3"
		@"Lnz//Q/Yvn0HvtmyRaVZfcHgJs76lHk8P578g1Y7uMNu/QAcY/TNK7plZKSjc6fOaNOmjexUSBhq"
		@"amqwr2hf3D8GZsCqCnflhFZ+1jK7PX0A49qWU72GEBL1AtD4AKez6tuWftjq5qAuV/VWDizTLy9C"
		@"iP7Y0tY6P3CaT/e0tDS7JcG0BwDdKickxjDAldDg73nw8GFna6855aM+n89Xl5JsqQLY5eLTI4To"
		@"iQGzy6qqTzn6LZjrey0rM2MTBztfUF6EEP1tdrorBwPgp3pRq/cAmgk0cu0WAEqPkiBEIUc4M92M"
		@"03R+IMjRfvX19eUpyZZygI2NODVCiK44wy0ul+fTYF4b9HDf2jrvt6lJSWeCoU/4qRFC9MSBZS53"
		@"5cJgXx/MJcC/X5xomQlwdXZGIEQte5lmvjOUN4Q8yKedzdbbz/gmAOmhvpcQopuqALT/cLvdu0N5"
		@"U0hnAABQ5vH8CA1XALw+1PcSQnTh44xPDLXzA2EUAABwOis/54xNwbGVRQkh0gQY+DUuV1VQN/1O"
		@"Fvac/7q6+sKUpORysPieMESITIzhzgp31Wvhvj+iRT9q6+u3paYk+QEMjyQOISR0nLGHnK7KRZHE"
		@"iHjVn9q6+i+Tk5IrGMMo0MxBQozgB2ezXG7PU5EGEtZh7XbrOMb5WwCjZXUI0QuDl3E+vcJd9Y6Y"
		@"cAI5HNahCGA16BEhIXqoCYBPcLurhO3fEdZTgNY4nZWfmzgbBHAl11QmRKLdAWgFIjs/ILgAAEfH"
		@"CSSlpJ0L4BXRsQmJSxzLTQmW88J5zn86ut60s9ut0xnH8wBS9WyHEEXVAfwup7tKt91Jdb9rb7PZ"
		@"8s0ssJSDDdS7LUJUwYGvwEwzXC6XrnNvjHpsx+x26zQGPAGOLIPaJCQWuRmwsMJd+SyAgN6NGbb7"
		@"T11d/fcJiZZXNI0lM2AAaMwAIc1xcLyZ2Oi/vKyqegOCWMxDBCmd0G5PH8CgzT227wAVAhLPODhW"
		@"w8QfPdXqvXqR2vlsNlu+Cfx+MFwNwCwzF0IMFgDDGjA+X0bHbxIVn74OR1oPBMy/B/hkABmy8yFE"
		@"R5UAW8FM/kUVFdU/y04mKgpAk66ApdJmuwQM0wA+DkCC7JwIEcAPYANnWG42W/5WVlZ2RHZCTaKq"
		@"ADTXtm1qO+5PuIoDl4DjQtDwYhJbqsDwJQf7OMHb8E5pTU2F7IRaErUF4CTmzMzM/ozxYYzz4QD6"
		@"AbDJToqQZjwM2BbgbD00/3qXq3o7YmDBnFgpAL/Rvk2bLH9CQk/OAj0ArTs4784ZshjgYICdHx3m"
		@"bEUM/xtJVOAAKhkQ4GBODu5iHBVgbC9DoBBcKzQ1NBRG6yf86fw/kvushTCCM9QAAAAASUVORK5C"
		@"YII="
		                                                   options:NSDataBase64DecodingIgnoreUnknownCharacters];
		img = [[NSImage alloc] initWithData:png];
	}
	return img;
}

@interface PWMAccessSheet : NSObject <NSWindowDelegate>
@property(nonatomic) int result;
@property(nonatomic) int finished;
@property(nonatomic) int evaluating;
@property(nonatomic, copy) NSString *why;
@property(nonatomic, strong) NSWindow *win;
@property(nonatomic, strong) LAContext *lac;
@end

@implementation PWMAccessSheet
- (void)finish:(int)res {
	if (self.finished) {
		return;
	}
	self.finished = 1;
	self.result = res;
	// stopModal only sets a flag — _doModalLoop checks it after dequeuing
	// an event. The sheet may already be ordered out (scan owns the
	// surface by then), so nothing is queued to wake it: the loop would
	// park in mach_msg forever and veil_access would never return.
	// Post a synthetic event to force one iteration.
	[self.win orderOut:nil];
	[NSApp stopModal];
	NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
		location:NSZeroPoint
		modifierFlags:0
		timestamp:0
		windowNumber:0
		context:nil
		subtype:0
		data1:0
		data2:0];
	[NSApp postEvent:wake atStart:YES];
	veil_confirm_log([NSString stringWithFormat:@"sheet finish=%d", res]);
}
- (void)finishEval:(NSNumber *)ok {
	self.evaluating = 0;
	[self finish:ok.intValue];
}
- (void)scan {
	// The sensor IS the button — the sheet explains who is asking while
	// evaluatePolicy runs. Cancel or a declined scan both finish 0.
	if (self.finished || self.evaluating) {
		return;
	}
	self.evaluating = 1;
	// The system Touch ID dialog can land on whichever Space the LA agent
	// binds to — which is not necessarily the one the human is watching.
	// Keep our sheet up for the whole eval so "Touch the sensor" is always
	// on screen; finish() orders it out when the eval settles.
	LAContext *ctx = self.lac;
	NSString *why = self.why;
	veil_confirm_log(@"scan: dispatching eval");
	dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
		int ok = veil_touchid_ctx(ctx, why.UTF8String);
		[self performSelectorOnMainThread:@selector(finishEval:)
			withObject:@(ok)
			waitUntilDone:NO
			modes:@[NSModalPanelRunLoopMode, NSDefaultRunLoopMode]];
	});
}
- (void)cancel:(id)sender {
	(void)sender;
	[self.lac invalidate];
	[self finish:0];
}
- (BOOL)windowShouldClose:(NSWindow *)sender {
	(void)sender;
	[self.lac invalidate];
	[self finish:0];
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

// The access sheet is the same window every time — only the asking-app
// icon, the verb line, and the account change. Build it once (the daemon's
// warm child pays this at spawn) and per request swap those three + reset.
static NSWindow *veil_sheet_win = nil;
static NSImageView *veil_sheet_left = nil;
static NSTextField *veil_sheet_allow = nil;
static NSTextField *veil_sheet_acct = nil;
static NSButton *veil_sheet_cancel = nil;

static void veil_sheet_once(void) {
	if (veil_sheet_win) {
		return;
	}
	NSRect frame = NSMakeRect(0, 0, 420, 288);
	NSWindow *win = [[NSWindow alloc] initWithContentRect:frame
		styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
			NSWindowStyleMaskNonactivatingPanel)
		backing:NSBackingStoreBuffered
		defer:NO];
	win.title = @"Veil Access Requested";
	win.level = NSModalPanelWindowLevel;
	// The requester (browser, terminal) may live on another Space than
	// the one the human is looking at — the sheet must follow the
	// human, not the requester, or the armed sensor reads as nothing
	// happening.
	win.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
		NSWindowCollectionBehaviorStationary |
		NSWindowCollectionBehaviorFullScreenAuxiliary;
	win.releasedWhenClosed = NO;
	veil_sheet_win = win;

	NSView *content = win.contentView;
	veil_sheet_left = veil_icon_view([NSImage imageWithSystemSymbolName:@"terminal" accessibilityDescription:nil]);
	NSImageView *check = [[NSImageView alloc] initWithFrame:NSZeroRect];
	NSApp.applicationIconImage = veil_veil_mark();
	check.image = [NSImage imageWithSystemSymbolName:@"checkmark.circle.fill" accessibilityDescription:nil];
	check.contentTintColor = [NSColor systemGreenColor];
	[check.widthAnchor constraintEqualToConstant:22].active = YES;
	[check.heightAnchor constraintEqualToConstant:22].active = YES;
	NSImageView *right = veil_icon_view(veil_veil_mark());
	NSStackView *icons = [NSStackView stackViewWithViews:@[veil_sheet_left, check, right]];
	icons.orientation = NSUserInterfaceLayoutOrientationHorizontal;
	icons.alignment = NSLayoutAttributeCenterY;
	icons.spacing = 16;

	veil_sheet_allow = [NSTextField labelWithString:@""];
	veil_sheet_allow.font = [NSFont systemFontOfSize:15 weight:NSFontWeightSemibold];
	veil_sheet_allow.alignment = NSTextAlignmentCenter;

	NSImageView *acctIcon = [[NSImageView alloc] initWithFrame:NSZeroRect];
	acctIcon.image = veil_veil_mark();
	acctIcon.wantsLayer = YES;
	acctIcon.layer.cornerRadius = 6;
	acctIcon.layer.masksToBounds = YES;
	[acctIcon.widthAnchor constraintEqualToConstant:28].active = YES;
	[acctIcon.heightAnchor constraintEqualToConstant:28].active = YES;
	veil_sheet_acct = [NSTextField labelWithString:@""];
	veil_sheet_acct.font = [NSFont systemFontOfSize:13 weight:NSFontWeightMedium];
	NSImageView *chev = [[NSImageView alloc] initWithFrame:NSZeroRect];
	chev.image = [NSImage imageWithSystemSymbolName:@"chevron.right" accessibilityDescription:nil];
	chev.contentTintColor = [NSColor tertiaryLabelColor];
	[chev.widthAnchor constraintEqualToConstant:12].active = YES;
	[chev.heightAnchor constraintEqualToConstant:12].active = YES;
	NSStackView *acctRow = [NSStackView stackViewWithViews:@[acctIcon, veil_sheet_acct, chev]];
	acctRow.orientation = NSUserInterfaceLayoutOrientationHorizontal;
	acctRow.alignment = NSLayoutAttributeCenterY;
	acctRow.spacing = 10;
	acctRow.edgeInsets = NSEdgeInsetsMake(8, 10, 8, 12);
	acctRow.wantsLayer = YES;
	acctRow.layer.cornerRadius = 8;
	acctRow.layer.backgroundColor = NSColor.controlBackgroundColor.CGColor;
	[veil_sheet_acct setContentHuggingPriority:NSLayoutPriorityDefaultLow forOrientation:NSLayoutConstraintOrientationHorizontal];
	[veil_sheet_acct setContentCompressionResistancePriority:NSLayoutPriorityDefaultLow forOrientation:NSLayoutConstraintOrientationHorizontal];

	veil_sheet_cancel = [NSButton buttonWithTitle:@"Cancel" target:nil action:@selector(cancel:)];
	veil_sheet_cancel.keyEquivalent = @"\e";
	// The sensor is the button — the scan is already running. The hint
	// exists so nobody waits on a control that would stack a second ask.
	NSImageView *finger = [[NSImageView alloc] init];
	finger.image = [[NSImage imageWithSystemSymbolName:@"touchid" accessibilityDescription:@"Touch ID"] imageWithSymbolConfiguration:[NSImageSymbolConfiguration configurationWithPointSize:18 weight:NSFontWeightRegular]];
	NSTextField *hint = [NSTextField labelWithString:@"Touch the sensor"];
	hint.textColor = NSColor.secondaryLabelColor;
	NSStackView *hintRow = [NSStackView stackViewWithViews:@[finger, hint]];
	hintRow.spacing = 8;
	hintRow.alignment = NSLayoutAttributeCenterY;
	NSStackView *btns = [NSStackView stackViewWithViews:@[hintRow, veil_sheet_cancel]];
	btns.orientation = NSUserInterfaceLayoutOrientationHorizontal;
	btns.alignment = NSLayoutAttributeCenterY;
	btns.distribution = NSStackViewDistributionEqualSpacing;
	btns.spacing = 12;

	NSStackView *stack = [NSStackView stackViewWithViews:@[icons, veil_sheet_allow, acctRow, btns]];
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
}

static void veil_warm_app(void) {
	// The --confirm-server child runs this at spawn: every per-request
	// veil_access then skips the 1.5s LaunchServices handshake +
	// sharedApplication boot + the ~1.3s first window build.
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
	[NSProcessInfo processInfo].automaticTerminationSupportEnabled = NO;
	[NSApp finishLaunching];
	veil_sheet_once();
	veil_confirm_log(@"warm appkit done");
}

static int veil_access(const char *action, const char *account, const char *reason) {
	__block int out = 0;
	void (^run)(void) = ^{
		veil_confirm_log(@"sheet run enter");
		[NSApplication sharedApplication];
		veil_confirm_log(@"sharedApplication done");
		[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
		veil_confirm_log(@"activationPolicy done");
		// finishLaunching makes this plist-less helper a managed app —
		// without the opt-out, efficiency termination can kill it mid-eval
		// and the daemon's socket reply never lands. The flag (not the
		// counter) sticks because AppKit unbalances the counter as windows
		// open and close.
		[NSProcessInfo processInfo].automaticTerminationSupportEnabled = NO;
		[NSApp finishLaunching];
		veil_confirm_log(@"finishLaunching done");
		PWMAccessSheet *ctrl = [[PWMAccessSheet alloc] init];
		ctrl.lac = [[LAContext alloc] init];
		veil_confirm_log(@"lac done");
		NSRunningApplication *client = veil_client_app();
		veil_confirm_log(@"client done");
		NSString *appName = client.localizedName.length ? client.localizedName : @"this app";
		NSImage *clientIcon = client.icon ?: [NSImage imageWithSystemSymbolName:@"terminal" accessibilityDescription:nil];
		NSString *allow = [NSString stringWithFormat:@"Allow %@ to %s", appName, action];
		NSString *who = [NSString stringWithUTF8String:account];
		// One story on every surface: the LA system dialog's reason is the
		// same ask the sheet shows — app, action, account — never a generic
		// second prompt competing with it.
		ctrl.why = who.length ? [NSString stringWithFormat:@"%@ — %@", allow, who] : allow;

		// The window is pooled: built once (the warm child pays it at
		// spawn) — per request swap the three request-specific bits and
		// rebind the per-call controller as delegate + cancel target.
		veil_sheet_once();
		NSWindow *win = veil_sheet_win;
		win.delegate = ctrl;
		ctrl.win = win;
		veil_sheet_cancel.target = ctrl;
		veil_sheet_left.image = clientIcon;
		veil_sheet_allow.stringValue = allow;
		veil_sheet_acct.stringValue = who;
		veil_confirm_log(@"sheet ready");
		// Never activate or take key: activating our process pulls focus
		// away from the requester — and for a Safari AutoFill/passkey
		// request, the OS cancels the provider handoff the moment the
		// host app loses key. orderFrontRegardless + CanJoinAllSpaces
		// puts the sheet on the human's Space without stealing it.
		// Eval first, surface second: ordering the pooled window costs up
		// to ~1s of compositor work, and scan() only arms a flag + hands
		// the eval to a global queue — so start the system prompt NOW and
		// let our sheet composite in parallel. The Touch ID dialog is the
		// thing the human is waiting on; the brand sheet can trail it the
		// way 1Password's trails the OS prompt.
		[ctrl scan];
		[win orderFrontRegardless];
		veil_confirm_log(@"ordered front");
		// A nonactivating window cannot own a modal session —
		// runModalForWindow returns immediately and the eval would be
		// orphaned. Pump the default mode until finish() settles instead.
		while (!ctrl.finished) {
			[[NSRunLoop currentRunLoop]
				runMode:NSDefaultRunLoopMode
				beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.25]];
		}
		veil_confirm_log(@"runloop exited");
		[win orderOut:nil];
		win.delegate = nil;
		[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		out = ctrl.result;
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
