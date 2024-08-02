import clsx from "clsx";

import { type positions } from "common/constants";

import { Question } from "icons";

interface TooltipProps {
  header: string;
  body: string;
  position: keyof typeof positions;
}

const Tooltip = ({ header, body, position }: TooltipProps) => {
  const isBottom = /^bottom/.test(position);
  const isLeft = /left$/.test(position);
  const yPosition = isBottom ? "top-[16px]" : "bottom-[16px]";
  const xPosition = isLeft ? "right-[16px]" : "left-[16px]";
  const peerHover = isBottom ? "peer-hover:translate-y-[11px]" : "peer-hover:translate-y-[-11px]";

  return (
    <div className="relative">
      <Question className="cursor-help peer" />
      <div
        className={clsx(
          "invisible opacity-0 peer-hover:visible peer-hover:opacity-100",
          "peer-hover:transition peer-hover:transform peer-hover:duration-200",
          "absolute bg-surface-base text-text-primary/100 p-4 rounded-lg w-80 shadow-200 z-10",
          yPosition,
          xPosition,
          peerHover
        )}
      >
        <div className="text-heading-100 text-text-primary mb-1">{header}</div>
        <div className="text-300 text-text-primary/70">{body}</div>
      </div>
    </div>
  );
};

export default Tooltip;
