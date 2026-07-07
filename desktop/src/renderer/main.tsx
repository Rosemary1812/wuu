import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./App";
import { applySkinPreference, applyThemePreference } from "./Theme";
import "./styles.css";

// The preload script already stamped data-theme/data-skin for the first
// paint; re-applying here takes over the "system" media-query
// subscription for the lifetime of the window.
applyThemePreference(window.wuu?.initialThemePreference ?? "system");
applySkinPreference(window.wuu?.initialSkinPreference ?? "flame");

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <App />,
);
