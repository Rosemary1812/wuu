import React from "react";
import ReactDOM from "react-dom/client";
// Electron uses the browser Workbench renderer as the shared source of truth.
import { App } from "../../../browser/agent/packages/workbench-ui/src/renderer/App";
import "overlayscrollbars/overlayscrollbars.css";
import "../../../browser/agent/packages/workbench-ui/src/renderer/styles.css";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <App />
);
