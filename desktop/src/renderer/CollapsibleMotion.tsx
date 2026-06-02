import type {
  CSSProperties,
  Dispatch,
  ReactNode,
  SetStateAction,
} from "react";
import { useEffect, useLayoutEffect, useRef, useState } from "react";

export function useAutoCollapseState({
  autoCollapse,
  defaultExpanded,
  forceExpanded = false,
  collapseDelay = 520,
}: {
  autoCollapse: boolean;
  defaultExpanded: boolean;
  forceExpanded?: boolean;
  collapseDelay?: number;
}): readonly [boolean, Dispatch<SetStateAction<boolean>>] {
  const [expanded, setExpanded] = useState(defaultExpanded);
  const previousAutoCollapseRef = useRef(autoCollapse);
  const mountedRef = useRef(false);

  useEffect(() => {
    const previousAutoCollapse = previousAutoCollapseRef.current;
    previousAutoCollapseRef.current = autoCollapse;
    const firstRun = !mountedRef.current;
    mountedRef.current = true;

    if (forceExpanded) {
      setExpanded(true);
      return undefined;
    }
    if (autoCollapse) {
      if (firstRun && !defaultExpanded) {
        setExpanded(false);
        return undefined;
      }
      setExpanded(true);
      const timer = window.setTimeout(() => setExpanded(false), collapseDelay);
      return () => window.clearTimeout(timer);
    }
    if (previousAutoCollapse) {
      setExpanded(true);
    }
    return undefined;
  }, [autoCollapse, collapseDelay, defaultExpanded, forceExpanded]);

  return [expanded, setExpanded] as const;
}

export function CollapsibleDetails({
  children,
  className,
  expanded,
  id,
  innerClassName,
}: {
  children: ReactNode;
  className?: string;
  expanded: boolean;
  id?: string;
  innerClassName?: string;
}): JSX.Element {
  const innerRef = useRef<HTMLDivElement | null>(null);
  const [height, setHeight] = useState(0);

  useLayoutEffect(() => {
    const node = innerRef.current;
    if (!node) {
      setHeight(0);
      return;
    }
    setHeight(node.scrollHeight);
  }, [children, expanded]);

  useEffect(() => {
    const node = innerRef.current;
    if (!node || !expanded) {
      return undefined;
    }
    const resizeObserver = new ResizeObserver(() => {
      setHeight(node.scrollHeight);
    });
    resizeObserver.observe(node);
    return () => resizeObserver.disconnect();
  }, [expanded]);

  const detailsClassName = [
    "collapsible-details",
    expanded ? "expanded" : "collapsed",
    className,
  ]
    .filter(Boolean)
    .join(" ");
  const innerClassNames = ["collapsible-details-inner", innerClassName]
    .filter(Boolean)
    .join(" ");
  const style = {
    height: expanded ? `${height}px` : "0px",
  } satisfies CSSProperties;

  return (
    <div
      className={detailsClassName}
      id={id}
      aria-hidden={!expanded}
      style={style}
    >
      <div className={innerClassNames} ref={innerRef}>
        {children}
      </div>
    </div>
  );
}
