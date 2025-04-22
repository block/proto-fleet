import clsx from "clsx";

import { Question } from "@/shared/assets/icons";
import { Position } from "@/shared/constants";

interface TooltipProps {
  header: string;
  body: string;
  position: Position;
}

const Tooltip = ({ header, body, position }: TooltipProps) => {
  const isBottom = /^bottom/.test(position);
  const isLeft = /left$/.test(position);
  const yPosition = isBottom ? "top-[16px]" : "bottom-[16px]";
  const xPosition = isLeft ? "right-[16px]" : "left-[16px]";
  const peerHover = isBottom
    ? "peer-hover:translate-y-[11px]"
    : "peer-hover:translate-y-[-11px]";

  return (
    <div className="relative">
      <Question className="peer cursor-help" />
      <div
        className={clsx(
          "invisible opacity-0 peer-hover:visible peer-hover:opacity-100",
          "peer-hover:transform peer-hover:transition peer-hover:duration-200",
          "absolute z-10 w-80 rounded-lg bg-surface-base p-4 text-text-primary shadow-200",
          yPosition,
          xPosition,
          peerHover,
        )}
      >
        <div className="mb-1 text-heading-100 text-text-primary">{header}</div>
        <div className="text-300 text-text-primary-70">{body}</div>
      </div>
    </div>
  );
};

export default Tooltip;
