import ApplicationServices
import Foundation

private final class AXChangeWaiter: @unchecked Sendable {
    private let lock = NSLock()
    private var didChange = false

    func signal() {
        lock.lock()
        didChange = true
        lock.unlock()
    }

    func changed() -> Bool {
        lock.lock()
        defer { lock.unlock() }
        return didChange
    }
}

private let axChangeCallback: AXObserverCallback = { _, _, _, refcon in
    guard let refcon else { return }
    Unmanaged<AXChangeWaiter>.fromOpaque(refcon).takeUnretainedValue().signal()
}

func waitForAccessibilityChange(application: AXUIElement, processID: pid_t, timeout: TimeInterval) throws {
    var observer: AXObserver?
    let createError = AXObserverCreate(processID, axChangeCallback, &observer)
    guard createError == .success, let observer else {
        throw ComputerError.operationFailed("create AXObserver failed with error \(createError.rawValue)")
    }
    let waiter = AXChangeWaiter()
    let refcon = Unmanaged.passUnretained(waiter).toOpaque()
    let notifications = [
        kAXFocusedWindowChangedNotification,
        kAXFocusedUIElementChangedNotification,
        kAXWindowCreatedNotification,
        kAXValueChangedNotification,
        kAXSelectedTextChangedNotification,
        kAXUIElementDestroyedNotification,
    ]
    for notification in notifications {
        _ = AXObserverAddNotification(observer, application, notification as CFString, refcon)
    }
    let source = AXObserverGetRunLoopSource(observer)
    let runLoop = CFRunLoopGetCurrent()
    CFRunLoopAddSource(runLoop, source, .defaultMode)
    defer {
        CFRunLoopRemoveSource(runLoop, source, .defaultMode)
        for notification in notifications {
            _ = AXObserverRemoveNotification(observer, application, notification as CFString)
        }
    }
    let deadline = Date(timeIntervalSinceNow: timeout)
    while !waiter.changed(), Date() < deadline {
        CFRunLoopRunInMode(.defaultMode, min(0.1, deadline.timeIntervalSinceNow), true)
    }
}
