import { useEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import MinerListComponent from "../MinerList";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { miners } from "@/protoFleet/features/fleetManagement/components/MinerList/stories/mocks";
import {
  allIssueMiners,
  allStatusMiners,
  errorMessages,
} from "@/protoFleet/features/fleetManagement/components/MinerList/stories/statusMocks";
import { useFleetStore } from "@/protoFleet/store";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

const meta: Meta<typeof MinerListComponent> = {
  title: "Proto Fleet/MinerList",
  component: MinerListComponent,
};

export default meta;
type Story = StoryObj<typeof MinerListComponent>;

// Helper component to set up store state
const MinerListWrapper = ({ minerList }: { minerList: MinerStateSnapshot[] }) => {
  const setMiners = useFleetStore((state) => state.fleet.setMiners);
  const setErrors = useFleetStore((state) => state.fleet.setErrors);

  useEffect(() => {
    setMiners(minerList);
    // Add error messages to normalized store
    const deviceIds = minerList.map((m) => m.deviceIdentifier);
    setErrors(errorMessages, "devices", deviceIds);
  }, [setMiners, setErrors, minerList]);

  const minerIds = minerList.map((miner) => miner.deviceIdentifier);

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <MinerListComponent title="Miners" minerIds={minerIds} onAddMiners={action("onAddMiners")} />
    </div>
  );
};

// ============================================================================
// Consolidated Story with All States and Issues
// ============================================================================

export const AllStatusesAndIssuesMinerList: Story = {
  render: () => {
    const allMiners = [...allStatusMiners, ...allIssueMiners];
    return (
      <div className="space-y-8">
        <div>
          <h2 className="mb-4 text-heading-300">All Statuses and Issues</h2>
          <MinerListWrapper minerList={allMiners} />
        </div>
      </div>
    );
  },
};

// ============================================================================
// Other Examples
// ============================================================================

export const OperationalMinerList: Story = {
  render: () => <MinerListWrapper minerList={miners} />,
};

export const EmptyMinerList: Story = {
  render: () => (
    <div>
      <MinerListComponent title="Miners" minerIds={[]} totalMiners={0} onAddMiners={action("onAddMiners")} />
    </div>
  ),
};
