import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "@browseros/workbench-ui/renderer/App";
import "overlayscrollbars/overlayscrollbars.css";
import "@browseros/workbench-ui/renderer/styles.css";

ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
  <App />
);
