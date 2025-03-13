import { ReactNode, useEffect, useRef, useState } from "react";
import clsx from "clsx";

import "./style.css";

const Dot = (props: { connecting?: boolean }) => {
  return (
    <div className="relative flex">
      {props.connecting ? (
        <span
          className={clsx(
            "absolute rounded-full inline-flex animate-dot-connecting size-1",
          )}
          style={{
            animationDelay: `${Math.random() * 4}s`,
          }}
        ></span>
      ) : (
        <span
          className={clsx(
            "relative rounded-full inline-flex bg-text-primary-30 size-1",
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

  const [columnsCount, setColumnsCount] = useState(15);
  const [rowsCount, setRowsCount] = useState(15);

  useEffect(() => {
    setTimeout(() => {
      updateSize();
    }, 0);

    function updateSize() {
      if (!containerRef.current) return;

      const { width } = containerRef.current.getBoundingClientRect();
      const windowHeight = window.innerHeight;

      const PIXELS_BETWEEN_DOTS = 40;
      const DOT_SIZE = 4;
      const PADDING = 40;
      const maxRows = Math.ceil(
        windowHeight / (PIXELS_BETWEEN_DOTS + DOT_SIZE),
      );

      setRowsCount(maxRows);
      setColumnsCount(Math.ceil((width - PADDING) / PIXELS_BETWEEN_DOTS));
    }

    window.addEventListener("resize", updateSize);

    return () => window.removeEventListener("resize", updateSize);
  }, []);

  return (
    <div ref={containerRef} className="relative h-full max-h-svh w-full p-5">
      {children}
      <div
        className={clsx(
          "grid gap-10 place-items-center mx-auto",
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
