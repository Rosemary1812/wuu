import type { KeyboardEvent as ReactKeyboardEvent } from "react";

const NAVIGATION_KEYS = new Set(["ArrowLeft", "ArrowRight", "Home", "End"]);

export function handleTabListKeyDown(event: ReactKeyboardEvent<HTMLElement>): void {
  if (
    !NAVIGATION_KEYS.has(event.key) ||
    event.altKey ||
    event.ctrlKey ||
    event.metaKey
  ) {
    return;
  }
  const eventTarget = event.target;
  if (!(eventTarget instanceof Element)) {
    return;
  }
  const currentTab = eventTarget.closest<HTMLButtonElement>('[role="tab"]');
  if (!currentTab || !event.currentTarget.contains(currentTab)) {
    return;
  }
  const tabs = Array.from(
    event.currentTarget.querySelectorAll<HTMLButtonElement>('[role="tab"]:not(:disabled)'),
  );
  const currentIndex = tabs.indexOf(currentTab);
  if (currentIndex < 0 || tabs.length < 2) {
    return;
  }

  let nextIndex: number;
  if (event.key === "Home") {
    nextIndex = 0;
  } else if (event.key === "End") {
    nextIndex = tabs.length - 1;
  } else if (event.key === "ArrowRight") {
    nextIndex = (currentIndex + 1) % tabs.length;
  } else {
    nextIndex = (currentIndex - 1 + tabs.length) % tabs.length;
  }

  event.preventDefault();
  const nextTab = tabs[nextIndex];
  nextTab.focus();
  nextTab.click();
}
