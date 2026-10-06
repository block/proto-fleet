import { useEffect, useMemo, useState } from "react";
import useCssVariable from "./useCssVariable";

interface WindowDimensions {
  width: number;
  height: number;
}

const getWindowDimensions = (): WindowDimensions => ({
  width: window.innerWidth,
  height: window.innerHeight,
});

const useWindowDimensions = () => {
  const phoneMaxWidth = useCssVariable("--phone-max-width");
  const tabletMaxWidth = useCssVariable("--tablet-max-width");
  const laptopMaxWidth = useCssVariable("--laptop-max-width");

  const [windowDimensions, setWindowDimensions] = useState(getWindowDimensions);

  useEffect(() => {
    const handleResize = () => {
      setWindowDimensions(getWindowDimensions());
    };

    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, []);

  return useMemo(
    () => ({
      height: windowDimensions.height,
      width: windowDimensions.width,
      isDesktop: windowDimensions.width > laptopMaxWidth,
      isLaptop: windowDimensions.width > tabletMaxWidth && windowDimensions.width <= laptopMaxWidth,
      isTablet: windowDimensions.width > phoneMaxWidth && windowDimensions.width <= tabletMaxWidth,
      isPhone: windowDimensions.width <= phoneMaxWidth,
    }),
    [windowDimensions.height, windowDimensions.width, laptopMaxWidth, phoneMaxWidth, tabletMaxWidth],
  );
};

export { useWindowDimensions };
