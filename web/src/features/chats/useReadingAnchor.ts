import { useCallback, useEffect, useRef } from "react";

type Anchor = { id: string; top: number };

export function useReadingAnchor(
  scroller: HTMLElement | null,
  enabled: boolean,
  windowKey: string,
) {
  const rows = useRef(new Set<HTMLDivElement>());
  const observer = useRef<ResizeObserver | null>(null);

  useEffect(() => {
    if (!scroller || !enabled) return;
    const viewport = scroller;
    let anchor: Anchor | undefined;
    let frame = 0;
    const sizes = new WeakMap<Element, number>();
    function remember() {
      if (frame) return;
      const top = viewport.getBoundingClientRect().top;
      const row =
        [...rows.current].find((row) => {
          const rect = row.getBoundingClientRect();
          return rect.top <= top && rect.bottom > top;
        }) ??
        [...rows.current].find((row) => row.getBoundingClientRect().top >= top);
      if (row?.dataset.messageId)
        anchor = {
          id: row.dataset.messageId,
          top: row.getBoundingClientRect().top - top,
        };
    }
    function restore() {
      frame = 0;
      const saved = anchor;
      if (!saved) return;
      const row = [...rows.current].find(
        (row) => row.dataset.messageId === saved.id,
      );
      if (!row) {
        remember();
        return;
      }
      const delta =
        row.getBoundingClientRect().top -
        viewport.getBoundingClientRect().top -
        saved.top;
      if (Math.abs(delta) > 0.5) viewport.scrollTop += delta;
      remember();
    }
    const resize = new ResizeObserver((entries) => {
      let changed = false;
      for (const entry of entries) {
        const previous = sizes.get(entry.target);
        const height =
          entry.borderBoxSize[0]?.blockSize ??
          entry.target.getBoundingClientRect().height;
        sizes.set(entry.target, height);
        if (previous !== undefined && Math.abs(previous - height) > 0.5)
          changed = true;
      }
      // Let Virtuoso apply its own measurement correction before correcting any remaining drift.
      if (changed && anchor && !frame) frame = requestAnimationFrame(restore);
      else if (!anchor) remember();
    });
    observer.current = resize;
    for (const row of rows.current) resize.observe(row);
    scroller.addEventListener("scroll", remember, { passive: true });
    // A replaced page window is positioned by Virtuoso before a new reading anchor is captured.
    frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => {
        frame = 0;
        remember();
      });
    });
    return () => {
      cancelAnimationFrame(frame);
      scroller.removeEventListener("scroll", remember);
      resize.disconnect();
      observer.current = null;
    };
  }, [scroller, enabled, windowKey]);

  return useCallback((row: HTMLDivElement | null) => {
    if (!row) return;
    rows.current.add(row);
    observer.current?.observe(row);
    return () => {
      rows.current.delete(row);
      observer.current?.unobserve(row);
    };
  }, []);
}
