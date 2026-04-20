import { RefObject, useEffect, useRef } from "react";

interface ClickOutsideProps {
  ref: RefObject<HTMLElement | null>;
  onClickOutside: () => void;
  // Optional array of selectors for elements that should be considered "inside"
  ignoreSelectors?: string[];
  // Optional function to determine if a click/touch should be ignored
  shouldIgnore?: (event: MouseEvent | TouchEvent) => boolean;
}

const useClickOutside = ({ ref, onClickOutside, ignoreSelectors = [], shouldIgnore }: ClickOutsideProps) => {
  const onClickOutsideRef = useRef(onClickOutside);
  const shouldIgnoreRef = useRef(shouldIgnore);
  const ignoreSelectorsRef = useRef(ignoreSelectors);

  useEffect(() => {
    onClickOutsideRef.current = onClickOutside;
    shouldIgnoreRef.current = shouldIgnore;
    ignoreSelectorsRef.current = ignoreSelectors;
  }, [onClickOutside, shouldIgnore, ignoreSelectors]);

  useEffect(() => {
    function handleClickOutside(event: MouseEvent | TouchEvent) {
      if (shouldIgnoreRef.current && shouldIgnoreRef.current(event)) {
        return;
      }

      const isInside = event.target instanceof Node && ref.current && ref.current.contains(event.target);

      const isInsideIgnored = ignoreSelectorsRef.current.some((selector) => {
        if (event.target instanceof Node) {
          if (event.target instanceof Element && event.target.matches(selector)) {
            return true;
          }

          const closest = event.target instanceof Element ? event.target.closest(selector) : null;
          return closest !== null;
        }
        return false;
      });

      if (!isInside && !isInsideIgnored) {
        onClickOutsideRef.current();
      }
    }

    document.addEventListener("mousedown", handleClickOutside);
    document.addEventListener("touchstart", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("touchstart", handleClickOutside);
    };
  }, [ref]);
};

export { useClickOutside };
