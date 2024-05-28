import { useEffect } from "react";

interface CloseOnEscProps {
  key: string;
  onKeyDown: () => void;
}

const useKeyDown = ({ key, onKeyDown }: CloseOnEscProps) => {
  useEffect(() => {
    const dismissOnEsc = (event: KeyboardEvent) => {
      if (event.key === key) {
        onKeyDown();
      }
    };

    document.addEventListener("keydown", dismissOnEsc, false);

    return () => {
      document.removeEventListener("keydown", dismissOnEsc, false);
    };
  }, [key, onKeyDown]);
};

export { useKeyDown };
