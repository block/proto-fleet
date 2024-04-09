import { useEffect, useState } from "react";

const getWindowDimensions = () => ({
  width: window.innerWidth,
  height: window.innerHeight,
});

const useWindowDimensions = () => {
  const [windowDimensions, setWindowDimensions] = useState(
    getWindowDimensions()
  );

  useEffect(() => {
    const handleResize = () => {
      setWindowDimensions(getWindowDimensions());
    };

    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, []);

  return {
    height: windowDimensions.height,
    width: windowDimensions.width,
    isDesktop: windowDimensions.width >= 1280,
    isLaptop: windowDimensions.width >= 960 && windowDimensions.width < 1280,
    isTablet: windowDimensions.width >= 632 && windowDimensions.width < 960,
    isPhone: windowDimensions.width < 632,
  };
};

export { useWindowDimensions };
