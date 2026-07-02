import type { ReactNode, Ref } from "react";

export function CollapsibleDetails({
  children,
  className,
  containerRef,
  expanded,
  id,
  innerClassName,
}: {
  children: ReactNode;
  className?: string;
  containerRef?: Ref<HTMLDivElement>;
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
    <div
      className={detailsClassName}
      id={id}
      aria-hidden={!expanded}
      ref={containerRef}
    >
      <div className={innerClassNames}>{children}</div>
    </div>
  );
}
