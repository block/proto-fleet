import { ElementType, useState } from "react";
import { MemoryRouter } from "react-router-dom";

import AsicTableComponent from "./AsicTable";
import { mockHashboardStats } from "./constants";
import { HashrateResponseHashratedata } from "@/protoOS/api/types";
import DurationSelector, {
  durations,
} from "@/shared/components/DurationSelector";

interface AsicTableProps {
  pending: boolean;
}

export const AsicTable = ({ pending }: AsicTableProps) => {
  const [duration, setDuration] = useState<
    HashrateResponseHashratedata["duration"]
  >(durations[0]);
  const [showPopover, setShowPopover] = useState<string | undefined>(undefined);

  return (
    <div>
      <div className="flex justify-end">
        <DurationSelector
          className="h-fit"
          duration={duration}
          onSelect={setDuration}
        />
      </div>
      <AsicTableComponent
        asics={pending ? [] : mockHashboardStats.asics}
        duration={duration}
        granularity="1m"
        hashboardSerialNumber={mockHashboardStats.hb_sn}
        pending={pending}
        showPopover={showPopover}
        setShowPopover={setShowPopover}
      />
    </div>
  );
};

export default {
  title: "Components/Asic Temperature/Asic Table",
  args: {
    pending: false,
  },
  argTypes: {
    pending: {
      control: {
        type: "boolean",
      },
    },
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
