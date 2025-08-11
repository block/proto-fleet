import { ReactNode, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import "./style.css";
import { useWindowDimensions } from "@/shared/hooks/useWindowDimensions";

const Dot = (props: { connecting?: boolean }) => {
  return (
    <div className="relative flex">
      {props.connecting ? (
        <span
          className={clsx(
            "animate-dot-connecting absolute inline-flex size-1 rounded-full",
          )}
          style={{
            animationDelay: `${Math.random() * 4}s`,
          }}
        ></span>
      ) : (
        <span
          className={clsx(
            "relative inline-flex size-1 rounded-full bg-text-primary-30",
            {
              "animate-dot-stale": !props.connecting,
              "!bg-core-accent-fill": props.connecting,
            },
          )}
          style={{
            animationDelay: `${Math.random() * 4}s`,
          }}
        ></span>
      )}
    </div>
  );
};

type AnimatedDotsBackgroundProps = {
  connecting?: boolean;
  children?: ReactNode;
  padding?: number;
  spacing?: number;
  dotSize?: number;
};

const AnimatedDotsBackground = ({
  connecting,
  children,
  padding = 40,
  spacing = 40,
  dotSize = 4,
}: AnimatedDotsBackgroundProps) => {
  const containerRef = useRef<HTMLDivElement>(null);

  const { width: wnWidth, height: wnHeight } = useWindowDimensions();

  const [columnsCount, setColumnsCount] = useState(
    Math.ceil((wnWidth - padding) / spacing),
  );
  const [rowsCount, setRowsCount] = useState(
    Math.ceil(wnHeight / (spacing + dotSize)),
  );

  useEffect(() => {
    updateSize();

    function updateSize() {
      if (!containerRef.current) return;

      const containerWidth = containerRef.current.offsetWidth;
      const containerHeight = containerRef.current.offsetHeight;

      const maxRows = Math.ceil(containerHeight / (spacing + dotSize));

      const maxCols = Math.ceil((containerWidth - padding) / spacing);

      setRowsCount(maxRows);
      setColumnsCount(maxCols);
    }

    window.addEventListener("resize", updateSize);

    return () => window.removeEventListener("resize", updateSize);
  }, [dotSize, padding, spacing]);

  return (
    <div
      ref={containerRef}
      className="relative h-full w-full overflow-visible bg-surface-base p-0.5"
    >
      {children}
      <div
        className={clsx(
          "mx-auto grid place-items-center gap-10",
          `grid-cols-24 grid-rows-${rowsCount}`,
        )}
        style={{
          gridTemplateColumns: `repeat(${columnsCount}, minmax(0, 1fr))`,
          gridTemplateRows: `repeat(${rowsCount}, minmax(0, 1fr))`,
        }}
      >
        {Array.from({ length: columnsCount * rowsCount }).map((_, i) => (
          <Dot key={i} connecting={connecting && Math.random() > 0.9} />
        ))}
      </div>
    </div>
  );
};

export default AnimatedDotsBackground;
