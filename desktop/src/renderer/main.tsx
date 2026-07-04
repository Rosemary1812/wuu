import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./App";
import { applyThemePreference } from "./Theme";
import "./styles.css";

// The preload script already stamped data-theme for the first paint;
// re-applying here takes over the "system" media-query subscription for
// the lifetime of the window.
applyThemePreference(window.wuu?.initialThemePreference ?? "system");

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <App />,
);
