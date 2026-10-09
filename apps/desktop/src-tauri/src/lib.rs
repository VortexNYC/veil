// Veil desktop — the vault SPA in a Tauri shell. The window stays hidden
// until LocalAuthentication passes: same gate as the fill path, so the app
// feels like Veil instead of a browser tab.
#[cfg(target_os = "macos")]
use block2::RcBlock;
#[cfg(target_os = "macos")]
use objc2::runtime::Bool;
#[cfg(target_os = "macos")]
use objc2_foundation::{NSError, NSString};
#[cfg(target_os = "macos")]
use objc2_local_authentication::{LAContext, LAPolicy};
use tauri::menu::{MenuBuilder, MenuItemBuilder};
use tauri::tray::TrayIconBuilder;
use tauri::{AppHandle, Manager};

/// Device-owner eval — Touch ID with the OS passcode fallback the system
/// gives it. Blocks the calling thread until the user answers; reply comes
/// back on LA's queue.
#[cfg(target_os = "macos")]
fn evaluate_owner(reason: &str) -> bool {
    let (tx, rx) = std::sync::mpsc::channel();
    let tx2 = tx.clone();
    let handler = RcBlock::new(move |ok: Bool, err: *mut NSError| {
        if !err.is_null() {
            let e = unsafe { &*err };
            eprintln!("veil: LA err {:?}", e.localizedDescription());
        }
        let _ = tx2.send(ok.as_bool());
    });
    let reason = NSString::from_str(reason);
    // The context must outlive the async reply — dropping it invalidates
    // the eval and the answer comes back canceled.
    let ctx = unsafe { LAContext::new() };
    unsafe {
        ctx.evaluatePolicy_localizedReason_reply(
            LAPolicy::DeviceOwnerAuthentication,
            &reason,
            &handler,
        );
    }
    // A closed channel is unreachable — LA always answers.
    let ok = rx.recv().unwrap_or(false);
    let _ = ctx;
    ok
}

#[cfg(not(target_os = "macos"))]
fn evaluate_owner(_reason: &str) -> bool {
    true
}

/// The window opens on a local splash so LA has a live app surface for its
/// dialog; the eval runs on a thread and success navigates to the vault.
fn gate(app: &AppHandle) {
    let app2 = app.clone();
    std::thread::spawn(move || {
        if evaluate_owner("Veil needs to confirm it's you") {
            if let Some(win) = app2.get_webview_window("main") {
                let _ = win.eval("location.href = 'https://app.veil.nyc'");
            }
        } else {
            // Fail closed — no owner, no vault.
            app2.exit(0);
        }
    });
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_opener::init())
        .setup(|app| {
            gate(app.handle());

            let open = MenuItemBuilder::with_id("open", "Open Veil").build(app)?;
            let quit = MenuItemBuilder::with_id("quit", "Quit Veil").build(app)?;
            let menu = MenuBuilder::new(app).items(&[&open, &quit]).build()?;
            TrayIconBuilder::with_id("veil-tray")
                .icon(app.default_window_icon().unwrap().clone())
                .menu(&menu)
                .on_menu_event(|app, event| match event.id().as_ref() {
                    "open" => {
                        if let Some(win) = app.get_webview_window("main") {
                            let _ = win.show();
                            let _ = win.set_focus();
                        }
                    }
                    "quit" => app.exit(0),
                    _ => {}
                })
                .build(app)?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("error while running Veil");
}
