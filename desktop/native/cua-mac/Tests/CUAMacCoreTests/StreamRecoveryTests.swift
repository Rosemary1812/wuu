@testable import CUAMacCore
import XCTest

final class StreamRecoveryTests: XCTestCase {
    func testCaptureStageOriginFallsOutsideEveryDisplay() {
        let displays = [
            CGRect(x: 0, y: 0, width: 1800, height: 1169),
            CGRect(x: 0, y: -900, width: 1440, height: 900),
        ]
        let origin = captureStageOffscreenOrigin(screenFrames: displays)
        let parkedWindow = CGRect(origin: origin, size: CGSize(width: 1002, height: 670))
        XCTAssertTrue(displays.allSatisfy { parkedWindow.intersection($0).isNull })
    }

    func testCaptureStageVisibleFractionUsesQuartzCoordinatesForAnUpperDisplay() {
        let displays = [
            CGRect(x: 0, y: 0, width: 1800, height: 1169),
            CGRect(x: 0, y: -900, width: 1440, height: 900),
        ]
        let upperDisplayWindow = CGRect(x: 100, y: -800, width: 1000, height: 700)
        XCTAssertEqual(
            captureStageVisibleFraction(windowFrame: upperDisplayWindow, screenFrames: displays),
            1,
            accuracy: 0.0001
        )
    }

    func testCaptureStageAcceptsSystemClampedWindowWithOnlyASliverVisible() {
        let displays = [CGRect(x: 0, y: 0, width: 1800, height: 1169)]
        let clamped = CGRect(x: 1760, y: 1086, width: 1002, height: 670)
        XCTAssertLessThan(captureStageVisibleFraction(windowFrame: clamped, screenFrames: displays), 0.01)
        XCTAssertGreaterThan(captureStageVisibleFraction(
            windowFrame: CGRect(x: 900, y: 400, width: 1002, height: 670),
            screenFrames: displays
        ), 0.5)
    }

    func testCaptureStageTargetPrefersRequestedThenFocusedBeforeLargestWindow() {
        let largest = CGRect(x: 20, y: 40, width: 1200, height: 800)
        let requested = CGRect(x: 500, y: 120, width: 320, height: 240)
        let focused = CGRect(x: 900, y: 300, width: 260, height: 180)
        let frames = [largest, requested.offsetBy(dx: 2, dy: 2), focused]

        XCTAssertEqual(
            captureStageTargetIndex(windowFrames: frames, requestedFrame: requested, focusedFrame: focused),
            1
        )
        XCTAssertEqual(
            captureStageTargetIndex(windowFrames: frames, requestedFrame: nil, focusedFrame: focused),
            2
        )
        XCTAssertEqual(
            captureStageTargetIndex(windowFrames: frames, requestedFrame: nil, focusedFrame: nil),
            0
        )
    }

    func testCaptureStageWindowAssignmentsUseStagedFramesForDuplicateWindows() {
        let firstOriginal = CGRect(x: 100, y: 100, width: 600, height: 400)
        let secondOriginal = CGRect(x: 900, y: 100, width: 600, height: 400)
        let firstParked = CGRect(x: 7600, y: 5200, width: 600, height: 400)

        XCTAssertNotEqual(
            captureStageWindowAssignments(
                currentFrames: [firstParked, secondOriginal],
                currentTitles: ["Document", "Document"],
                expectedFrames: [firstOriginal, secondOriginal],
                expectedTitles: ["Document", "Document"]
            ).compactMap { $0 },
            [0, 1]
        )
        XCTAssertEqual(
            captureStageWindowAssignments(
                currentFrames: [firstParked, secondOriginal],
                currentTitles: ["Document", "Document"],
                expectedFrames: [firstParked, secondOriginal],
                expectedTitles: ["Document", "Document"]
            ).compactMap { $0 },
            [0, 1]
        )
        XCTAssertEqual(
            captureStageWindowAssignments(
                currentFrames: [secondOriginal],
                currentTitles: ["Document"],
                expectedFrames: [firstParked, secondOriginal],
                expectedTitles: ["Document", "Document"]
            ).compactMap { $0 },
            [1]
        )
    }

    func testIdleCallbacksKeepAStaticStreamHealthy() {
        var freshness = CaptureFreshness()
        freshness.recordCallback(at: 1, displayedFrame: true, captureUnavailable: false)
        freshness.recordCallback(at: 9, displayedFrame: false, captureUnavailable: false)

        XCTAssertFalse(freshness.requiresRecovery(
            at: 11,
            streamStartedAt: 0,
            startupTimeout: 5,
            callbackSilenceTimeout: 3,
            unavailableStatusTimeout: 3
        ))
        XCTAssertEqual(freshness.lastHealthyCallbackUptime, 9)
    }

    func testSilentStreamRecoversAfterItPreviouslyDisplayedAFrame() {
        var freshness = CaptureFreshness()
        freshness.recordCallback(at: 1, displayedFrame: true, captureUnavailable: false)

        XCTAssertTrue(freshness.requiresRecovery(
            at: 4,
            streamStartedAt: 0,
            startupTimeout: 5,
            callbackSilenceTimeout: 3,
            unavailableStatusTimeout: 3
        ))
    }

    func testAStreamThatNeverStartsStillRequiresRecovery() {
        let freshness = CaptureFreshness()

        XCTAssertTrue(freshness.requiresRecovery(
            at: 5,
            streamStartedAt: 0,
            startupTimeout: 5,
            callbackSilenceTimeout: 3,
            unavailableStatusTimeout: 3
        ))
    }

    func testPersistentUnavailableStatusRequiresRecovery() {
        var freshness = CaptureFreshness()
        freshness.recordCallback(at: 1, displayedFrame: true, captureUnavailable: false)
        freshness.recordCallback(at: 2, displayedFrame: false, captureUnavailable: true)
        freshness.recordCallback(at: 4, displayedFrame: false, captureUnavailable: true)

        XCTAssertTrue(freshness.requiresRecovery(
            at: 5,
            streamStartedAt: 0,
            startupTimeout: 5,
            callbackSilenceTimeout: 3,
            unavailableStatusTimeout: 3
        ))
        XCTAssertEqual(freshness.unavailableSinceUptime, 2)
    }

    func testHealthyCallbackClearsUnavailableStatus() {
        var freshness = CaptureFreshness()
        freshness.recordCallback(at: 1, displayedFrame: true, captureUnavailable: false)
        freshness.recordCallback(at: 2, displayedFrame: false, captureUnavailable: true)
        freshness.recordCallback(at: 4, displayedFrame: false, captureUnavailable: false)

        XCTAssertNil(freshness.unavailableSinceUptime)
        XCTAssertFalse(freshness.requiresRecovery(
            at: 6,
            streamStartedAt: 0,
            startupTimeout: 5,
            callbackSilenceTimeout: 3,
            unavailableStatusTimeout: 3
        ))
    }
}
