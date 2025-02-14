import { RefObject, useEffect } from "react";

interface ClickOutsideProps {
  ref: RefObject<HTMLElement>;
  onClickOutside: () => void;
}

const useClickOutside = ({ ref, onClickOutside }: ClickOutsideProps) => {
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        event.target instanceof Node &&
        ref.current &&
        !ref.current.contains(event.target)
      ) {
        onClickOutside();
      }
    }

    document.addEventListener("mousedown", handleClickOutside);
    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [onClickOutside, ref]);
};

export { useClickOutside };
