// SideThreadOpenBus — 极简模块级事件桥，避免把 `onOpenSideThread`
// 沿 ComposerView → SplitPaneComposer → ConversationSplitPane → ...
// 一路往下打洞。
//
// V1 范围：只支持一个全局"打开/切换侧聊"信号；不传负载、不持久化。
// 当 V1 验收后，下一步可以把调用方收敛回 prop 链，去掉这个总线。

type Listener = () => void;

const listeners = new Set<Listener>();

export function emitSideThreadOpenRequest(): void {
  for (const listener of listeners) {
    listener();
  }
}

export function subscribeSideThreadOpenRequest(listener: Listener): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}