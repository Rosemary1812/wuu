// The Wuu app-server protocol types live in packages/protocol — the single
// source of truth shared by every shell (desktop today, mobile and web
// clients next). This module re-exports them so desktop code keeps its
// existing `shared/protocol` import path.
export * from "../../../packages/protocol/src/index";
