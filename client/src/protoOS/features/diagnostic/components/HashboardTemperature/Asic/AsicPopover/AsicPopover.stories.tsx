import { ElementType } from "react";
import { mockHashboardStats } from "../constants";
import AsicPopoverComponent from "./AsicPopover";
import { PopoverProvider, usePopover } from "@/shared/components/Popover";

interface AsicPopoverStoryProps {
  asicIndex: number;
}

export const AsicPopover = ({ asicIndex }: AsicPopoverStoryProps) => {
  const { triggerRef } = usePopover();

  // Get the ASIC at the specified index from mock data
  const asic = mockHashboardStats.asics[asicIndex] || mockHashboardStats.asics[0];

  return (
    <div ref={triggerRef} className="relative mt-96 ml-40">
      <AsicPopoverComponent asic={asic} closePopover={() => {}} />
    </div>
  );
};

export default {
  title: "Proto OS/Asic Popover",
  decorators: [
    (Story: ElementType) => (
      <PopoverProvider>
        <Story />
      </PopoverProvider>
    ),
  ],
  args: {
    asicIndex: 0,
  },
  argTypes: {
    asicIndex: {
      control: {
        type: "number",
        min: 0,
        max: 99,
      },
      description: "Index of the ASIC to display (0-99)",
    },
  },
};
