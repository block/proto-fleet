import { RefObject, useEffect } from "react";

interface ClickOutsideProps {
  ref: RefObject<HTMLElement | null>;
  onClickOutside: () => void;
  // Optional array of selectors for elements that should be considered "inside"
  ignoreSelectors?: string[];
  // Optional function to determine if a click/touch should be ignored
  shouldIgnore?: (event: MouseEvent | TouchEvent) => boolean;
}

const useClickOutside = ({ ref, onClickOutside, ignoreSelectors = [], shouldIgnore }: ClickOutsideProps) => {
  useEffect(() => {
    function handleClickOutside(event: MouseEvent | TouchEvent) {
      // Skip if we should ignore this event
      if (shouldIgnore && shouldIgnore(event)) {
        return;
      }

      // Check if the click is inside our element
      const isInside = event.target instanceof Node && ref.current && ref.current.contains(event.target);

      // Check if the click is inside any of the elements matching ignoreSelectors
      const isInsideIgnored = ignoreSelectors.some((selector) => {
        if (event.target instanceof Node) {
          // Check if the target matches the selector
          if (event.target instanceof Element && event.target.matches(selector)) {
            return true;
          }

          // Check if any parent matches the selector
          const closest = event.target instanceof Element ? event.target.closest(selector) : null;
          return closest !== null;
        }
        return false;
      });

      // Only call onClickOutside if the click is outside all monitored elements
      if (!isInside && !isInsideIgnored) {
        onClickOutside();
      }
    }

    document.addEventListener("mousedown", handleClickOutside);
    document.addEventListener("touchstart", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
      document.removeEventListener("touchstart", handleClickOutside);
    };
  }, [onClickOutside, ref, ignoreSelectors, shouldIgnore]);
};

export { useClickOutside };
