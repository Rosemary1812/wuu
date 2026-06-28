import { useEffect, useRef, useState, type JSX } from "react";

const PROCESS_TEXT_EXIT_MS = 180;

export function AnimatedProcessText({
  text,
  className,
}: {
  text: string;
  className?: string;
}): JSX.Element {
  const previousText = useRef(text);
  const [exitingText, setExitingText] = useState<string | undefined>();

  useEffect(() => {
    if (previousText.current === text) {
      return undefined;
    }
    setExitingText(previousText.current);
    previousText.current = text;
    const timeoutID = window.setTimeout(() => {
      setExitingText(undefined);
    }, PROCESS_TEXT_EXIT_MS);
    return () => window.clearTimeout(timeoutID);
  }, [text]);

  return (
    <span
      className={["process-text-motion", className].filter(Boolean).join(" ")}
      data-transitioning={exitingText ? "true" : undefined}
    >
      {exitingText ? (
        <span
          aria-hidden="true"
          className="process-text-motion-copy process-text-motion-exit"
        >
          {exitingText}
        </span>
      ) : null}
      <span
        className={`process-text-motion-copy process-text-motion-current${
          exitingText ? " process-text-motion-enter" : ""
        }`}
        key={text}
      >
        {text}
      </span>
    </span>
  );
}
