import type { ReactNode } from "react";

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
