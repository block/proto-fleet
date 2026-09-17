import { useMemo } from "react";
import { componentLabels, components } from "./constants";
import { ComponentFilterType } from "./types";
import { useModuleModel } from "@/protoOS/store";
import SegmentedControl from "@/shared/components/SegmentedControl";
import type { Segment } from "@/shared/components/SegmentedControl/types";

interface ComponentSelectorProps {
  selectedComponent?: ComponentFilterType;
  onSelect?: (component: ComponentFilterType) => void;
}

const ComponentSelector = ({ selectedComponent, onSelect }: ComponentSelectorProps) => {
  const moduleModel = useModuleModel();
  const componentSegments = useMemo(() => {
    return components
      .filter((component) => moduleModel !== "ct1" || component !== "controlBoard")
      .map((component: ComponentFilterType) => ({
        key: component,
        title: componentLabels[component],
      })) as Segment[];
  }, [moduleModel]);

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
