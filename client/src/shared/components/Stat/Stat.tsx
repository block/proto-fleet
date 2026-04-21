import { motion } from "motion/react";
import { useEffect, useState } from "react";
import clsx from "clsx";
import SkeletonBar from "../SkeletonBar";
import Tooltip from "../Tooltip/Tooltip";
import { ChartStatus, chartStatus as chartStatusConstants, statusColors } from "./constants";
import { type StatProps } from "./types";
import useCssVariable from "@/shared/hooks/useCssVariable";
import { cubicBezierValues } from "@/shared/utils/cssUtils";
import { getDisplayValue } from "@/shared/utils/stringUtils";

const Stat = ({
  label,
  value,
  text,
  subtitle,
  tooltipContent,
  units,
  size,
  icon,
  headingLevel = 3,
  chartPercentage,
  chartStatus = chartStatusConstants.neutral as ChartStatus,
  valueClassName,
}: StatProps) => {
  // initially set scale to 0 for animation
  const [chartScale, setChartScale] = useState<number>(0);
  const easeGentle = useCssVariable("--ease-gentle", cubicBezierValues);

  useEffect(() => {
    if (!chartPercentage) return;
    requestAnimationFrame(() => {
      setChartScale(chartPercentage / 100);
    });
  }, [chartPercentage]);

  return (
    <div className="relative grid">
      <div className="flex items-center justify-between gap-2">
        <div
          role="heading"
          aria-level={headingLevel}
          className={clsx(
            "text-heading-50 text-text-primary-50 transition-opacity duration-500",
            value === undefined ? "opacity-30" : "opacity-100",
          )}
        >
          {label}
        </div>
        {subtitle && (
          <div
            className={clsx(
              "flex items-center gap-2 text-heading-50 text-text-primary-50 transition-opacity duration-500",
              value === undefined ? "opacity-30" : "opacity-100",
            )}
            role="status"
            aria-label="Data reporting status"
          >
            <span>{subtitle}</span>
            {tooltipContent && <Tooltip body={tooltipContent} position="bottom left" icon="info" />}
          </div>
        )}
      </div>
      {value === undefined ? (
        <SkeletonBar
          className={clsx(
            "w-32 py-1",
            size === "large" && "h-10",
            size === "medium" && "h-7",
            size === "small" && "h-5",
          )}
        />
      ) : (
        <motion.div
          animate={{ opacity: 1 }}
          initial={{ opacity: 0 }}
          transition={{ duration: 0.5, ease: easeGentle }}
          className={clsx(
            "overflow-hidden overflow-ellipsis whitespace-nowrap text-text-primary",
            size === "large" && "text-heading-300",
            size === "medium" && "text-heading-200",
            size === "small" && "text-heading-100",
          )}
        >
          <span className={valueClassName}>{getDisplayValue(value)}</span> {units && units}
        </motion.div>
      )}
      {icon && <div className="absolute top-0 right-0">{icon}</div>}
      {text && <div className="mt-1 text-300 text-text-primary-70">{text}</div>}
      {chartPercentage !== undefined && (
        <div className="relative mt-2 h-[2px] w-full">
          <div className={clsx("absolute h-full w-full opacity-20", statusColors[chartStatus])}></div>
          <div
            className={clsx(
              "absolute h-full w-full origin-left transition-transform duration-600 ease-gentle",
              statusColors[chartStatus],
            )}
            style={{ transform: `scaleX(${chartScale})` }}
            data-testid="stat-chart"
          ></div>
        </div>
      )}
    </div>
  );
};

export default Stat;
