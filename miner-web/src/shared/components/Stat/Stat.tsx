import { motion } from "motion/react";
import { useEffect, useState } from "react";
import clsx from "clsx";
import SkeletonBar from "../SkeletonBar";
import { statusColors } from "./constants";
import { type StatProps } from "./types";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const Stat = ({
  label,
  value,
  text,
  units,
  size,
  icon,
  headingLevel = 3,
  chartPercentage,
  chartStatus = "neutral",
}: StatProps) => {
  // initially set scale to 0 for animation
  const [chartScale, setChartScale] = useState<number>(0);

  const easeGentle = useCssVariable({
    variable: "--ease-gentle",
    transform: cubicBezierValues,
  });

  useEffect(() => {
    if (!chartPercentage) return;
    requestAnimationFrame(() => {
      setChartScale(chartPercentage / 100);
    });
  }, [chartPercentage]);

  return (
    <div className="relative">
      <div
        role="heading"
        aria-level={headingLevel}
        className={clsx(
          "text-heading-50 transition-opacity duration-500",
          value === undefined ? "opacity-30" : "opacity-100",
        )}
      >
        {label}
      </div>
      {value === undefined ? (
        <SkeletonBar className="h-7 py-1" />
      ) : (
        <motion.div
          animate={{ opacity: 1 }}
          initial={{ opacity: 0 }}
          transition={{ duration: 0.5, ease: easeGentle }}
          className={clsx(
            "overflow-hidden overflow-ellipsis whitespace-nowrap text-text-primary",
            size === "large" && "text-heading-300",
            size === "small" && "text-heading-200",
          )}
        >
          {getDisplayValue(value)}{" "}
          {units && <span className="text-text-primary-30">{units}</span>}
        </motion.div>
      )}
      {icon && <div className="absolute top-0 right-0">{icon}</div>}
      {text && <div className="text-300 text-text-primary-30">{text}</div>}
      {chartPercentage && (
        <div className="relative mt-2 h-[2px] w-full">
          <div
            className={clsx(
              "absolute h-full w-full opacity-20",
              statusColors[chartStatus],
            )}
          ></div>
          <div
            className={clsx(
              "absolute h-full w-full origin-left transition-transform duration-600 ease-gentle",
              statusColors[chartStatus],
            )}
            style={{ transform: `scaleX(${chartScale})` }}
          ></div>
        </div>
      )}
    </div>
  );
};

export default Stat;
