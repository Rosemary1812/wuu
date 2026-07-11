import React from "react";
import ReactDOM from "react-dom/client";
import { MESSAGE_FLOW_FONT_SIZE_RANGE } from "../shared/protocol";
import { App } from "./App";
import { applyMessageFlowFontSize } from "./MessageFlowFontSizeSection";
import { applyThemePreference } from "./Theme";
import "./styles.css";

// The preload script already stamped data-theme for the first paint;
// re-applying here takes over the "system" media-query subscription for
// the lifetime of the window.
applyThemePreference(window.wuu?.initialThemePreference ?? "system");

// Re-apply the message-stream font size in case the preload stamp was
// dropped (e.g. user unset window.wuu during boot, or the file was
// corrupted). Cheap and idempotent.
applyMessageFlowFontSize(
  window.wuu?.initialMessageFlowFontSize ?? MESSAGE_FLOW_FONT_SIZE_RANGE.default,
);

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <App />,
);
