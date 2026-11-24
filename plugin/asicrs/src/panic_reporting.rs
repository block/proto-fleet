use std::io::Write;

/// Install once at startup: the default hook prints panic payloads before
/// catch_unwind can discard firmware responses containing credentials.
pub fn install() {
    std::panic::set_hook(Box::new(|info| {
        let mut stderr = std::io::stderr().lock();
        if let Some(location) = info.location() {
            let _ = writeln!(
                stderr,
                "asicrs-plugin panicked at {}:{}:{}; diagnostic payload suppressed",
                location.file(),
                location.line(),
                location.column()
            );
        } else {
            let _ = writeln!(
                stderr,
                "asicrs-plugin panicked; diagnostic payload suppressed"
            );
        }
    }));
}
