import { useEffect, useState } from "react";

const usePreventScroll = () => {
  const initialOverflow = "scroll";
  const [overflow, setOverflow] = useState(initialOverflow);

  useEffect(() => {
    document.body.style.overflow = initialOverflow;

    return () => {
      document.body.style.overflow = initialOverflow;
    };
  }, []);

  useEffect(() => {
    if (overflow !== document.body.style.overflow) {
      document.body.style.overflow = overflow;
    }
  }, [overflow]);

  return {
    preventScroll: () => setOverflow("hidden"),
  };
};

export { usePreventScroll };
