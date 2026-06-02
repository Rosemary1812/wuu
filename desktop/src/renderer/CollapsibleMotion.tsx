import type {
  Dispatch,
  ReactNode,
  SetStateAction,
} from "react";
import { useEffect, useRef, useState } from "react";

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

  return (
    <div className={detailsClassName} id={id} aria-hidden={!expanded}>
      <div className={innerClassNames}>{children}</div>
    </div>
  );
}
