import { useCallback, useRef, useState } from "react";

import { useClickOutside } from "common/hooks/useClickOutside";

const useTooltip = () => {
  const [tooltipData, setTooltipData] = useState({
    x: 0,
    y: 0,
    payload: [] as any[],
  });
  const [isTooltipActive, setTooltipActive] = useState(false);
  const tooltipRef = useRef(null);

  const onClickOutside = useCallback(() => {
    setTooltipActive(false);
    setTooltipData({ x: 0, y: 0, payload: [] });
  }, []);

  useClickOutside({ ref: tooltipRef, onClickOutside });

  return {
    tooltipData,
    setTooltipData,
    isTooltipActive,
    setTooltipActive,
    tooltipRef,
  };
};

export { useTooltip };
