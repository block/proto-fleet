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

  for (let i = 0; i < stack.length; i++) {
    const frame = stack[i].current;
    if (!frame) continue;
    if (frame.shouldIgnore?.(event)) continue;

    const el = frame.ref.current;
    if (el && targetNode && el.contains(targetNode)) continue;

    if (targetMatchesSelectors(target, frame.ignoreSelectors)) continue;

    // Clicks inside any higher layer shield this layer from dismissal.
    let insideHigher = false;
    for (let j = i + 1; j < stack.length; j++) {
      const higher = stack[j].current;
      const higherEl = higher?.ref.current;
      if (higherEl && targetNode && higherEl.contains(targetNode)) {
        insideHigher = true;
        break;
      }
    }
    if (insideHigher) continue;

    frame.onDismiss();
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
 * Click-outside dismiss that respects modal nesting. Each active layer
 * pushes a frame onto a shared stack. Clicks inside any frame above this
 * one are treated as inside — preventing a parent modal from dismissing
 * when a nested modal is clicked.
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
