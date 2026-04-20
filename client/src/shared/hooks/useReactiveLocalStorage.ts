import { useCallback, useEffect, useState } from "react";

// TODO: Now that we've started using Zustand, this is no longer necessary.
// Move usages of this hook to Zustand store instead.
function useReactiveLocalStorage<T>(key: string, initialValue?: T): [T, (value: T | ((val: T) => T)) => void] {
  const [storedValue, setStoredValue] = useState<T>(() => {
    try {
      const item = localStorage.getItem(key);
      return item ? JSON.parse(item) : initialValue;
    } catch (error) {
      console.error(`Error reading localStorage key "${key}":`, error);
      return initialValue as T;
    }
  });

  const setValue = useCallback(
    (value: T | ((val: T) => T)) => {
      try {
        const valueToStore = value instanceof Function ? value(storedValue) : value;
        setStoredValue(valueToStore);
        localStorage.setItem(key, JSON.stringify(valueToStore));
        window.dispatchEvent(
          new CustomEvent("localStorageChange", {
            detail: { key, value: valueToStore },
          }),
        );
      } catch (error) {
        console.error(`Error setting localStorage key "${key}":`, error);
      }
    },
    [key, storedValue],
  );

  useEffect(() => {
    const handleStorageChange = (e: StorageEvent | CustomEvent) => {
      const changeKey = e instanceof StorageEvent ? e.key : (e as CustomEvent).detail?.key;

      if (changeKey === key) {
        try {
          const newValue =
            e instanceof StorageEvent
              ? e.newValue
                ? JSON.parse(e.newValue)
                : initialValue
              : (e as CustomEvent).detail?.value;
          setStoredValue(newValue);
        } catch (error) {
          console.error(`Error parsing localStorage key "${key}":`, error);
        }
      }
    };

    window.addEventListener("storage", handleStorageChange);
    window.addEventListener("localStorageChange", handleStorageChange as (event: Event) => void);

    return () => {
      window.removeEventListener("storage", handleStorageChange);
      window.removeEventListener("localStorageChange", handleStorageChange as (event: Event) => void);
    };
  }, [key, initialValue]);

  return [storedValue, setValue];
}

export { useReactiveLocalStorage };
