import { useCallback } from "react";

const useLocalStorage = () => {
  const setItem = useCallback((key: string, value: any) => {
    localStorage.setItem(key, JSON.stringify(value));
  }, []);

  const getItem = useCallback((key: string) => {
    const localStorageValue = localStorage.getItem(key) || "";
    return localStorageValue ? JSON.parse(localStorageValue) : undefined;
  }, []);

  return {
    setItem,
    getItem,
  };
};

export { useLocalStorage };
