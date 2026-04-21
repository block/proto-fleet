import { action } from "storybook/actions";
import SegmentedControlComponent from "@/shared/components/SegmentedControl/SegmentedControl";
import { type Segment } from "@/shared/components/SegmentedControl/types";

export const SegmentedControl = () => {
  const segments = [
    {
      key: "active",
      title: "Active",
    },
    {
      key: "long",
      title: "Segment with a very long title",
    },
    {
      key: "archive",
      title: "Archive",
    },
  ] as Segment[];

  const handleSelect = (selectedKey: string) => {
    action("Segment selected")(selectedKey);
  };

  return <SegmentedControlComponent segments={segments} onSelect={handleSelect} />;
};

export default {
  title: "Shared/Segmented Control",
};
