import { KeyboardEvent, Ref } from "react";
import clsx from "clsx";
import { Segment as SegmentType } from "@/shared/components/SegmentedControl/types";

interface SegmentProps {
  segmentRef: Ref<HTMLButtonElement>;
  className?: string;
  segment: SegmentType;
  selected?: boolean;
  onSelect: () => void;
}

const Segment = ({ segmentRef, className, segment, selected = false, onSelect }: SegmentProps) => {
  const onKeyDown = (key: string) => {
    if (key === "Enter") {
      onSelect();
    }
  };

  return (
    <button
      ref={segmentRef}
      className={clsx("relative rounded-3xl", {
        "text-emphasis-200": selected,
        "text-200 text-text-primary-30": !selected,
      })}
      onMouseDown={onSelect}
      onKeyDown={(e: KeyboardEvent<HTMLButtonElement>) => onKeyDown(e.key)}
    >
      <div className={clsx("relative z-10 px-3 py-1", className)}>{segment.title}</div>
    </button>
  );
};

export default Segment;
