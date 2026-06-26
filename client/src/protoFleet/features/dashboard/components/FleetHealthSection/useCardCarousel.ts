import { type RefObject, useCallback, useEffect, useLayoutEffect, useRef, useState } from "react";

export interface CardCarousel {
  viewportRef: RefObject<HTMLDivElement | null>;
  trackRef: RefObject<HTMLDivElement | null>;
  /** Pixels to shift the track left, clamped so the last card sits flush right. */
  translatePx: number;
  /** True when the track is wider than its viewport (cards overflow). */
  hasOverflow: boolean;
  canPrev: boolean;
  canNext: boolean;
  prev: () => void;
  next: () => void;
}

// Headless horizontal carousel for a fixed-width card row. Measures the
// viewport/track (and the first card, for the per-step distance) so it works
// for any card size, and re-measures on resize or when `contentKey` changes
// (tab switch, item count). Stepping advances one card; the translate is
// clamped to the max scroll so the final step right-aligns the last card.
export function useCardCarousel(contentKey: unknown): CardCarousel {
  const viewportRef = useRef<HTMLDivElement>(null);
  const trackRef = useRef<HTMLDivElement>(null);
  const [index, setIndex] = useState(0);
  const [maxScroll, setMaxScroll] = useState(0);
  const [step, setStep] = useState(0);

  const measure = useCallback(() => {
    const viewport = viewportRef.current;
    const track = trackRef.current;
    if (!viewport || !track) return;
    setMaxScroll(Math.max(0, track.scrollWidth - viewport.clientWidth));
    const firstCard = track.firstElementChild as HTMLElement | null;
    const gap = parseFloat(getComputedStyle(track).columnGap) || 0;
    setStep(firstCard ? firstCard.getBoundingClientRect().width + gap : 0);
  }, []);

  // Reset to the start when the content changes — the render-time
  // adjust-state-on-change pattern (no effect), so it lands before paint.
  const [prevKey, setPrevKey] = useState(contentKey);
  if (contentKey !== prevKey) {
    setPrevKey(contentKey);
    setIndex(0);
  }

  // Re-measure after layout so the arrows reflect the new row before paint.
  useLayoutEffect(() => {
    measure();
  }, [measure, contentKey]);

  // Track size changes (cards finishing layout, container resize) re-measure.
  useEffect(() => {
    const viewport = viewportRef.current;
    const track = trackRef.current;
    if (!viewport || !track || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => measure());
    observer.observe(viewport);
    observer.observe(track);
    return () => observer.disconnect();
  }, [measure, contentKey]);

  const maxIndex = step > 0 ? Math.ceil((maxScroll - 1) / step) : 0;
  const safeIndex = Math.min(index, Math.max(0, maxIndex));
  const translatePx = Math.min(safeIndex * step, maxScroll);

  return {
    viewportRef,
    trackRef,
    translatePx,
    hasOverflow: maxScroll > 1,
    canPrev: safeIndex > 0,
    canNext: safeIndex < maxIndex,
    prev: () => setIndex(Math.max(0, safeIndex - 1)),
    next: () => setIndex(Math.min(maxIndex, safeIndex + 1)),
  };
}
