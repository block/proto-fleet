import { useEffect, useState } from "react";

declare global {
  interface Window {
    __themeObserver?: MutationObserver;
    __themeObserverCallbacks?: Set<() => void>;
  }
}

/**
 * Custom hook to query the value of a CSS variable at a given element scope
 * and listen for changes to the `data-theme` attribute.
 *
 * @param variable - CSS variable name
 * @param transform - Function to transform the value of the CSS variable
 * @param scope - Element at which the CSS variable value is being queried
 * @returns value of the CSS variable
 */
const useCssVariable = (variable: string, transform?: (v: string) => any, scope: Element = document.body) => {
  const [value, setValue] = useState(() => {
    const v = window.getComputedStyle(scope).getPropertyValue(variable);
    return transform ? transform(v) : v;
  });

  useEffect(() => {
    const updateValue = () => {
      const v = window.getComputedStyle(scope).getPropertyValue(variable);
      setValue(transform ? transform(v) : v);
    };

    // Initial update
    updateValue();

    // Initialize the callbacks Set if it doesn't exist
    if (!window.__themeObserverCallbacks) {
      window.__themeObserverCallbacks = new Set();
    }

    // Add this component's update callback to the set
    window.__themeObserverCallbacks.add(updateValue);

    // Initialize the observer if it doesn't exist
    if (!window.__themeObserver) {
      const observer = new MutationObserver((mutations) => {
        for (const mutation of mutations) {
          if (mutation.type === "attributes" && mutation.attributeName === "data-theme") {
            // When theme changes, call all registered callbacks
            window.__themeObserverCallbacks?.forEach((callback) => callback());
          }
        }
      });

      observer.observe(document.body, {
        attributes: true,
        attributeFilter: ["data-theme"],
      });

      window.__themeObserver = observer;
    }

    // Cleanup: remove this component's callback when unmounting
    return () => {
      window.__themeObserverCallbacks?.delete(updateValue);

      // If there are no more callbacks, disconnect the observer
      if (window.__themeObserverCallbacks?.size === 0) {
        window.__themeObserver?.disconnect();
        window.__themeObserver = undefined;
        window.__themeObserverCallbacks = undefined;
      }
    };
  }, [variable, transform, scope]);

  return value;
};

export default useCssVariable;
