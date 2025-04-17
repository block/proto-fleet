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
};

const AnimatedDotsBackground = ({
  connecting,
  children,
}: AnimatedDotsBackgroundProps) => {
  const containerRef = useRef<HTMLDivElement>(null);

  const PIXELS_BETWEEN_DOTS = 40;
  const DOT_SIZE = 4;
  const PADDING = 40;

  const { width, height } = useWindowDimensions();

  const [columnsCount, setColumnsCount] = useState(
    Math.ceil((width - PADDING) / PIXELS_BETWEEN_DOTS),
  );
  const [rowsCount, setRowsCount] = useState(
    Math.ceil(height / (PIXELS_BETWEEN_DOTS + DOT_SIZE)),
  );

  useEffect(() => {
    updateSize();

    function updateSize() {
      if (!containerRef.current) return;

      const maxRows = Math.ceil(height / (PIXELS_BETWEEN_DOTS + DOT_SIZE));
      const maxCols = Math.ceil((width - PADDING) / PIXELS_BETWEEN_DOTS);

      setRowsCount(maxRows);
      setColumnsCount(maxCols);
    }

    window.addEventListener("resize", updateSize);

    return () => window.removeEventListener("resize", updateSize);
  }, [height, width]);

  return (
    <div
      ref={containerRef}
      className="relative h-svh w-full overflow-hidden p-5"
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
