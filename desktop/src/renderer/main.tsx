import ReactDOM from "react-dom/client";
import { MESSAGE_FLOW_FONT_SIZE_RANGE } from "../shared/protocol";
import { App } from "./App";
import { applyMessageFlowFontSize } from "./MessageFlowFontSizeSection";
import { applyPlatformStamp } from "./platform";
import { applyThemePreference } from "./Theme";
import "./styles.css";

// The preload script already stamped data-theme for the first paint;
// re-applying here takes over the "system" media-query subscription for
// the lifetime of the window.
applyThemePreference(window.wuu?.initialThemePreference ?? "system");

// Same story for data-platform: the preload stamps it pre-paint; this
// covers boots whose preload was replaced (e2e mocks).
applyPlatformStamp();

// Re-apply the message-stream font size in case the preload stamp was
// dropped (e.g. user unset window.wuu during boot, or the file was
// corrupted). Cheap and idempotent.
applyMessageFlowFontSize(
  window.wuu?.initialMessageFlowFontSize ?? MESSAGE_FLOW_FONT_SIZE_RANGE.default,
);

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <App />,
);
