import { useEffect, useRef, useState, type JSX, type RefObject } from "react";
import { motionDurationMs, prefersReducedMotion } from "./motion";

export type CrystallizeOrbProps = {
  sourceRef: RefObject<HTMLElement | null>;
  targetRef: RefObject<HTMLElement | null>;
  pulsing?: boolean;
  onComplete?: () => void;
};

export function CrystallizeOrb({
  sourceRef,
  targetRef,
  pulsing,
  onComplete,
}: CrystallizeOrbProps): JSX.Element | null {
  const [orbStyle, setOrbStyle] = useState<{
    top: number;
    left: number;
    width: number;
    height: number;
  } | null>(null);
  const [visible, setVisible] = useState(false);
  const completeCalledRef = useRef(false);

  useEffect(() => {
    const source = sourceRef.current;
    const target = targetRef.current;
    if (!source || !target) {
      onComplete?.();
      return;
    }

    const sourceRect = source.getBoundingClientRect();
    const targetRect = target.getBoundingClientRect();
    const startWidth = Math.min(Math.max(sourceRect.width * 0.4, 32), 96);
    const startHeight = Math.min(Math.max(sourceRect.height * 0.4, 32), 96);
    const startTop = sourceRect.top + sourceRect.height / 2;
    const startLeft = sourceRect.left + sourceRect.width / 2;
    const endTop = targetRect.top + targetRect.height / 2;
    const endLeft = targetRect.left + targetRect.width / 2;

    source.classList.add("kanban-crystallize-source-condensed");
    setOrbStyle({ top: startTop, left: startLeft, width: startWidth, height: startHeight });

    const raf = requestAnimationFrame(() => {
      setVisible(true);
      setOrbStyle({ top: endTop, left: endLeft, width: 24, height: 24 });
    });

    const duration = motionDurationMs("--motion-slower", 440);
    const timer = window.setTimeout(() => {
      if (completeCalledRef.current) return;
      completeCalledRef.current = true;
      source.classList.remove("kanban-crystallize-source-condensed");
      onComplete?.();
    }, prefersReducedMotion() ? 0 : duration);

    return () => {
      cancelAnimationFrame(raf);
      window.clearTimeout(timer);
      source.classList.remove("kanban-crystallize-source-condensed");
    };
  }, [sourceRef, targetRef, onComplete]);

  if (!orbStyle) return null;
  return (
    <div
      className={`kanban-crystallize-orb${visible ? " visible" : ""}${pulsing ? " pulsing" : ""}`}
      style={{
        top: orbStyle.top,
        left: orbStyle.left,
        width: orbStyle.width,
        height: orbStyle.height,
      }}
    />
  );
}
