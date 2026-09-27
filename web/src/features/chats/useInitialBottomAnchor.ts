import { type RefObject, useCallback, useEffect, useRef } from "react";
import type { VirtuosoHandle } from "react-virtuoso";

export function useInitialBottomAnchor(
  list: RefObject<VirtuosoHandle | null>,
  scroller: HTMLElement | null,
  enabled: boolean,
) {
  const active = useRef(enabled);
  const frame = useRef(0);

  const release = useCallback(() => {
    active.current = false;
    cancelAnimationFrame(frame.current);
  }, []);

  const settle = useCallback(() => {
    if (!active.current || !enabled) return;
    cancelAnimationFrame(frame.current);
    // Media can change row heights after Virtuoso finishes its initial scroll.
    frame.current = requestAnimationFrame(() => {
      if (active.current)
        list.current?.scrollToIndex({ index: "LAST", align: "end" });
    });
  }, [enabled, list]);

  useEffect(() => {
    if (!enabled) release();
  }, [enabled, release]);

  useEffect(() => {
    if (!scroller) return;
    const events = ["wheel", "touchstart", "pointerdown", "keydown"] as const;
    for (const event of events)
      scroller.addEventListener(event, release, { passive: true });
    const resize = new ResizeObserver(settle);
    resize.observe(scroller);
    settle();
    return () => {
      cancelAnimationFrame(frame.current);
      resize.disconnect();
      for (const event of events) scroller.removeEventListener(event, release);
    };
  }, [scroller, release, settle]);

  return settle;
}
