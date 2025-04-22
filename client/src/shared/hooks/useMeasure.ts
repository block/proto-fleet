import { useCallback, useLayoutEffect, useMemo, useState } from "react";

export type UseMeasureRect = Pick<
  DOMRectReadOnly,
  "x" | "y" | "top" | "left" | "right" | "bottom" | "height" | "width"
>;
export type UseMeasureRef<E extends Element = Element> = (element: E) => void;
export type UseMeasureResult<E extends Element = Element> = [
  UseMeasureRef<E>,
  UseMeasureRect,
  UseMeasureRect,
];

const defaultState: UseMeasureRect = {
  x: 0,
  y: 0,
  width: 0,
  height: 0,
  top: 0,
  left: 0,
  bottom: 0,
  right: 0,
};

function useMeasure<E extends Element = Element>(): UseMeasureResult<E> {
  const [element, ref] = useState<E | null>(null);
  const [contentRect, setContentRect] = useState<UseMeasureRect>(defaultState);
  const [boundingRect, setBoundingRect] =
    useState<UseMeasureRect>(defaultState);

  const setElementBoundingRect = useCallback(() => {
    if (element) {
      const { x, y, width, height, top, left, bottom, right } =
        element.getBoundingClientRect();
      setBoundingRect({ x, y, width, height, top, left, bottom, right });
    }
  }, [element]);

  const observer = useMemo(
    () =>
      new ResizeObserver((entries) => {
        if (entries[0]) {
          const { x, y, width, height, top, left, bottom, right } =
            entries[0].contentRect;
          setContentRect({ x, y, width, height, top, left, bottom, right });
        }
        setElementBoundingRect();
      }),
    [setElementBoundingRect],
  );

  useLayoutEffect(() => {
    if (!element) return;
    observer.observe(element);
    setElementBoundingRect();
    return () => {
      observer.disconnect();
    };
  }, [element, observer, setElementBoundingRect]);

  return [ref, contentRect, boundingRect];
}

export default useMeasure;
