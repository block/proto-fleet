import { useMemo } from "react";
import { componentLabels, components } from "./constants";
import { ComponentFilterType } from "./types";
import SegmentedControl from "@/shared/components/SegmentedControl";
import type { Segment } from "@/shared/components/SegmentedControl/types";

interface ComponentSelectorProps {
  selectedComponent?: ComponentFilterType;
  onSelect?: (component: ComponentFilterType) => void;
}

const ComponentSelector = ({ selectedComponent, onSelect }: ComponentSelectorProps) => {
  const componentSegments = useMemo(() => {
    return components.map((component: ComponentFilterType) => ({
      key: component,
      title: componentLabels[component],
    })) as Segment[];
  }, []);

  return (
    <SegmentedControl
      className="h-fit"
      segments={componentSegments}
      initialSegmentKey={selectedComponent}
      onSelect={(key) => onSelect && onSelect(key as ComponentFilterType)}
    />
  );
};

export default ComponentSelector;
