import { useCallback, useMemo } from "react";

const useLocalStorage = () => {
  const setItem = useCallback((key: string, value: any, ttl?: number) => {
    const item = {
      value,
      ...(ttl && { expiry: Date.now() + ttl }),
    };
    localStorage.setItem(key, JSON.stringify(item));
  }, []);

  const getItem = useCallback((key: string) => {
    const localStorageValue = localStorage.getItem(key) || "";
    if (!localStorageValue) return undefined;

    try {
      const item = JSON.parse(localStorageValue);

      // Handle legacy items that don't have the new structure
      if (typeof item !== "object" || item === null || !("value" in item)) {
        return item;
      }

      // Check if item has expired
      if (item.expiry && Date.now() > item.expiry) {
        localStorage.removeItem(key);
        return undefined;
      }

      return item.value;
    } catch {
      return undefined;
    }
  }, []);

  return useMemo(
    () => ({
      setItem,
      getItem,
    }),
    [setItem, getItem],
  );
};

export { useLocalStorage };
