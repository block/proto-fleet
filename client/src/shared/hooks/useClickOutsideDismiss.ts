import { type RefObject, useEffect, useRef } from "react";

interface Frame {
  ref: RefObject<HTMLElement | null>;
  onDismiss: () => void;
  ignoreSelectors: string[];
  shouldIgnore?: (event: MouseEvent | TouchEvent) => boolean;
}

type FrameRef = RefObject<Frame | undefined>;

const stack: FrameRef[] = [];
let listenerInstalled = false;

const targetMatchesSelectors = (target: EventTarget | null, selectors: string[]): boolean => {
  if (!(target instanceof Element)) return false;
  return selectors.some((selector) => target.matches(selector) || target.closest(selector) !== null);
};

const handleDocumentPointerDown = (event: MouseEvent | TouchEvent) => {
  const target = event.target;
  const targetNode = target instanceof Node ? target : null;

  for (let i = stack.length - 1; i >= 0; i--) {
    const frame = stack[i].current;
    if (!frame) continue;
    if (frame.shouldIgnore?.(event)) continue;

    const el = frame.ref.current;
    if (el && targetNode && el.contains(targetNode)) return;

    if (targetMatchesSelectors(target, frame.ignoreSelectors)) return;

    frame.onDismiss();
    return;
  }
};

const ensureListener = () => {
  if (listenerInstalled) return;
  document.addEventListener("mousedown", handleDocumentPointerDown);
  document.addEventListener("touchstart", handleDocumentPointerDown);
  listenerInstalled = true;
};

interface ClickOutsideDismissProps {
  ref: RefObject<HTMLElement | null>;
  onDismiss: (() => void) | undefined;
  ignoreSelectors?: string[];
  shouldIgnore?: (event: MouseEvent | TouchEvent) => boolean;
}

/**
 * Click-outside dismiss that respects modal nesting. Active layers push a
 * frame onto a shared stack; a single outside click only dismisses the
 * topmost frame. This mirrors the Escape stack and avoids cascade-close
 * when nested overlays are rendered through portals (where a child's own
 * backdrop is not contained by the parent's ref).
 *
 * Pass `onDismiss: undefined` to unregister (e.g. when the modal is closed).
 */
const useClickOutsideDismiss = ({ ref, onDismiss, ignoreSelectors = [], shouldIgnore }: ClickOutsideDismissProps) => {
  const frameRef = useRef<Frame | undefined>(undefined);
  useEffect(() => {
    frameRef.current = onDismiss ? { ref, onDismiss, ignoreSelectors, shouldIgnore } : undefined;
  });

  const registered = onDismiss !== undefined;

  useEffect(() => {
    if (!registered) return;
    ensureListener();
    stack.push(frameRef);
    return () => {
      const idx = stack.lastIndexOf(frameRef);
      if (idx !== -1) stack.splice(idx, 1);
    };
  }, [registered]);
};

export const __resetClickOutsideStackForTests = () => {
  stack.length = 0;
};

export { useClickOutsideDismiss };
