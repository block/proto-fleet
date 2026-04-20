import { useEffect, useRef } from "react";

interface CloseOnEscProps {
  key?: string;
  onKeyDown: (event: KeyboardEvent) => void;
}

const useKeyDown = ({ key, onKeyDown }: CloseOnEscProps) => {
  const onKeyDownRef = useRef(onKeyDown);

  useEffect(() => {
    onKeyDownRef.current = onKeyDown;
  }, [onKeyDown]);

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (key) {
        if (event.key === key) {
          onKeyDownRef.current(event);
        }
      } else {
        onKeyDownRef.current(event);
      }
    };

    document.addEventListener("keydown", handleKeyDown, false);

    return () => {
      document.removeEventListener("keydown", handleKeyDown, false);
    };
  }, [key]);
};

export { useKeyDown };
