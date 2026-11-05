import { useEffect, useRef, useState } from "react";
import {
  clamp,
  constrainView,
  fitScale,
  fitView,
  MAX_ZOOM,
  minimumScale,
  type Point,
  resizeView,
  type View,
  type ViewportSize,
  zoomView,
} from "../components/systemDiagramView";

type Gesture =
  | { kind: "pan"; pointer: Point; view: View; scrollElement: Element; scrollTop: number }
  | { kind: "pinch"; distance: number; scale: number; anchor: Point };

interface Snapshot {
  view: View;
  size: ViewportSize;
  dragging: boolean;
}

function scrollParent(element: HTMLElement): Element {
  for (let parent = element.parentElement; parent; parent = parent.parentElement) {
    if (/(auto|scroll)/.test(getComputedStyle(parent).overflowY) && parent.scrollHeight > parent.clientHeight)
      return parent;
  }
  return document.scrollingElement ?? document.documentElement;
}

export function useSchematicViewport() {
  const [viewport, setViewport] = useState<HTMLDivElement | null>(null);
  const [snapshot, setSnapshot] = useState<Snapshot>({
    view: { x: 0, y: 0, scale: 1 },
    size: { width: 0, height: 0 },
    dragging: false,
  });
  const actions = useRef<{ fit: () => void; zoomBy: (factor: number) => void } | null>(null);

  useEffect(() => {
    if (!viewport) return;
    let view: View = { x: 0, y: 0, scale: 1 };
    let size: ViewportSize = { width: 0, height: 0 };
    let gesture: Gesture | undefined;
    let moved = false;
    let ignoreClickUntil = 0;
    const pointers = new Map<number, Point>();
    const publish = (dragging = false) => setSnapshot({ view, size, dragging });
    const fit = () => {
      if (!size.width) return;
      view = fitView(size);
      publish();
    };
    const zoom = (scale: number, anchor?: Point) => {
      if (!size.width) return;
      view = zoomView(view, size, scale, anchor);
      publish();
    };
    actions.current = { fit, zoomBy: (factor) => zoom(view.scale * factor) };

    const beginGesture = () => {
      const [first, second] = [...pointers.values()];
      moved = false;
      if (second) {
        const rect = viewport.getBoundingClientRect();
        const center = { x: (first.x + second.x) / 2 - rect.left, y: (first.y + second.y) / 2 - rect.top };
        gesture = {
          kind: "pinch",
          distance: Math.max(1, Math.hypot(first.x - second.x, first.y - second.y)),
          scale: view.scale,
          anchor: { x: (center.x - view.x) / view.scale, y: (center.y - view.y) / view.scale },
        };
        moved = true;
      } else if (first) {
        const scrollElement = scrollParent(viewport);
        gesture = { kind: "pan", pointer: first, view, scrollElement, scrollTop: scrollElement.scrollTop };
      } else gesture = undefined;
    };
    const clearGesture = () => {
      for (const id of pointers.keys()) {
        if (viewport.hasPointerCapture(id)) viewport.releasePointerCapture(id);
      }
      pointers.clear();
      gesture = undefined;
      moved = false;
    };
    const pointerDown = (event: PointerEvent) => {
      if (!size.width || (event.pointerType === "mouse" && event.button !== 0)) return;
      if (!pointers.size) ignoreClickUntil = 0;
      if (!(event.target as Element).closest("button, a, input, select, textarea"))
        viewport.focus({ preventScroll: true });
      pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
      beginGesture();
    };
    const pointerMove = (event: PointerEvent) => {
      if (!pointers.has(event.pointerId) || !gesture) return;
      pointers.set(event.pointerId, { x: event.clientX, y: event.clientY });
      const [first, second] = [...pointers.values()];
      if (gesture.kind === "pinch" && second) {
        const rect = viewport.getBoundingClientRect();
        const scale = clamp(
          (gesture.scale * Math.hypot(first.x - second.x, first.y - second.y)) / gesture.distance,
          minimumScale(size),
          MAX_ZOOM,
        );
        view = constrainView(
          {
            scale,
            x: (first.x + second.x) / 2 - rect.left - gesture.anchor.x * scale,
            y: (first.y + second.y) / 2 - rect.top - gesture.anchor.y * scale,
          },
          size,
        );
        publish(true);
      } else if (gesture.kind === "pan") {
        const dx = event.clientX - gesture.pointer.x;
        const dy = event.clientY - gesture.pointer.y;
        if (!moved && Math.hypot(dx, dy) < 6) return;
        moved = true;
        // Capture only after dragging starts, so a simple equipment click keeps its original target.
        viewport.setPointerCapture(event.pointerId);
        if (event.pointerType === "touch" && view.scale <= fitScale(size) + 0.001) {
          gesture.scrollElement.scrollTop = gesture.scrollTop - dy;
        } else {
          view = constrainView({ ...gesture.view, x: gesture.view.x + dx, y: gesture.view.y + dy }, size);
          publish(true);
        }
      }
    };
    const pointerUp = (event: PointerEvent) => {
      if (!pointers.has(event.pointerId)) return;
      if (moved) ignoreClickUntil = performance.now() + 350;
      pointers.delete(event.pointerId);
      if (viewport.hasPointerCapture(event.pointerId)) viewport.releasePointerCapture(event.pointerId);
      beginGesture();
      publish();
    };
    const cancel = () => {
      if (moved) ignoreClickUntil = performance.now() + 350;
      clearGesture();
      publish();
    };
    const click = (event: MouseEvent) => {
      if (event.detail > 0 && performance.now() < ignoreClickUntil) {
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    };
    const wheel = (event: WheelEvent) => {
      if (!event.ctrlKey && !event.metaKey) return;
      event.preventDefault();
      const rect = viewport.getBoundingClientRect();
      const delta = event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? size.height : 1);
      zoom(view.scale * Math.exp(-clamp(delta, -150, 150) * 0.01), {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      });
    };
    const keyDown = (event: KeyboardEvent) => {
      if (!size.width || event.target !== viewport || event.ctrlKey || event.metaKey || event.altKey) return;
      if (event.key === "+" || event.key === "=") zoom(view.scale * 1.35);
      else if (event.key === "-") zoom(view.scale / 1.35);
      else if (event.key === "0") fit();
      else if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(event.key)) {
        view = constrainView(
          {
            ...view,
            x: view.x + (event.key === "ArrowLeft" ? 48 : event.key === "ArrowRight" ? -48 : 0),
            y: view.y + (event.key === "ArrowUp" ? 48 : event.key === "ArrowDown" ? -48 : 0),
          },
          size,
        );
        publish();
      } else return;
      event.preventDefault();
    };
    const focusIn = (event: FocusEvent) => {
      const target = event.target as HTMLElement;
      if (!size.width || target === viewport || !target.matches(":focus-visible")) return;
      const canvas = viewport.getBoundingClientRect();
      const control = target.getBoundingClientRect();
      if (
        control.left >= canvas.left &&
        control.right <= canvas.right &&
        control.top >= canvas.top &&
        control.bottom <= canvas.bottom
      )
        return;
      view = constrainView(
        {
          ...view,
          x: view.x + canvas.left + size.width / 2 - (control.left + control.width / 2),
          y: view.y + canvas.top + size.height / 2 - (control.top + control.height / 2),
        },
        size,
      );
      publish();
    };
    const observer = new ResizeObserver(([entry]) => {
      const next = { width: entry.contentRect.width, height: entry.contentRect.height };
      if (next.width <= 32 || next.height <= 32 || (next.width === size.width && next.height === size.height)) return;
      clearGesture();
      view = size.width ? resizeView(view, size, next) : fitView(next);
      size = next;
      publish();
    });
    observer.observe(viewport);
    viewport.addEventListener("pointerdown", pointerDown);
    viewport.addEventListener("click", click, true);
    // React's delegated wheel listener is passive; this listener must cancel browser zoom for canvas gestures.
    viewport.addEventListener("wheel", wheel, { passive: false });
    viewport.addEventListener("keydown", keyDown);
    viewport.addEventListener("focusin", focusIn);
    window.addEventListener("pointermove", pointerMove);
    window.addEventListener("pointerup", pointerUp);
    window.addEventListener("pointercancel", cancel);
    window.addEventListener("blur", cancel);
    return () => {
      clearGesture();
      observer.disconnect();
      actions.current = null;
      viewport.removeEventListener("pointerdown", pointerDown);
      viewport.removeEventListener("click", click, true);
      viewport.removeEventListener("wheel", wheel);
      viewport.removeEventListener("keydown", keyDown);
      viewport.removeEventListener("focusin", focusIn);
      window.removeEventListener("pointermove", pointerMove);
      window.removeEventListener("pointerup", pointerUp);
      window.removeEventListener("pointercancel", cancel);
      window.removeEventListener("blur", cancel);
    };
  }, [viewport]);

  return {
    ...snapshot,
    viewportRef: setViewport,
    fit: () => actions.current?.fit(),
    zoomIn: () => actions.current?.zoomBy(1.35),
    zoomOut: () => actions.current?.zoomBy(1 / 1.35),
  };
}
