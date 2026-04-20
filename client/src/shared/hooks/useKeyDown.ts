import { useEffect } from "react";

interface CloseOnEscProps {
  key?: string;
  onKeyDown: (event: KeyboardEvent) => void;
}

const useKeyDown = ({ key, onKeyDown }: CloseOnEscProps) => {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (key) {
        if (event.key === key) {
          onKeyDown(event);
        }
      } else {
        onKeyDown(event);
      }
    };

    document.addEventListener("keydown", handleKeyDown, false);

    return () => {
      document.removeEventListener("keydown", handleKeyDown, false);
    };
  }, [key, onKeyDown]);
};

export { useKeyDown };
