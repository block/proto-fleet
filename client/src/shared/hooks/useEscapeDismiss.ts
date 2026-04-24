import { type RefObject, useEffect, useRef } from "react";

type EscapeHandler = () => void;
type HandlerRef = RefObject<EscapeHandler | undefined>;

const stack: HandlerRef[] = [];
let listenerInstalled = false;

const handleDocumentKeyDown = (event: KeyboardEvent) => {
  if (event.key !== "Escape") return;
  const top = stack[stack.length - 1];
  const handler = top?.current;
  if (!handler) return;
  event.stopPropagation();
  event.preventDefault();
  handler();
};

const ensureListener = () => {
  if (listenerInstalled) return;
  document.addEventListener("keydown", handleDocumentKeyDown, false);
  listenerInstalled = true;
};

/**
 * Escape-to-dismiss that respects nesting. Active consumers push a frame
 * onto a shared stack; only the topmost frame fires on Escape, so a child
 * modal/dialog/sheet consumes the event before a parent sees it.
 *
 * Pass `undefined` to unregister — e.g. when the modal is closed or is a
 * layer that shouldn't participate in Escape dismissal. To keep a frame on
 * the stack but swallow Escape (e.g. while a parent is busy), pass a no-op
 * function instead.
 */
const useEscapeDismiss = (onDismiss: EscapeHandler | undefined) => {
  const ref = useRef<EscapeHandler | undefined>(undefined);
  useEffect(() => {
    ref.current = onDismiss;
  });

  const registered = onDismiss !== undefined;

  useEffect(() => {
    if (!registered) return;
    ensureListener();
    stack.push(ref);
    return () => {
      const idx = stack.lastIndexOf(ref);
      if (idx !== -1) stack.splice(idx, 1);
    };
  }, [registered]);
};

export const __resetEscapeStackForTests = () => {
  stack.length = 0;
};

export { useEscapeDismiss };
