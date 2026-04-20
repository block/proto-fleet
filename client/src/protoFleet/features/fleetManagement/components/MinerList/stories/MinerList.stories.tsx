import type { Meta, StoryObj } from "@storybook/react";
import { action } from "storybook/actions";
import MinerListComponent from "../MinerList";
import type { ErrorMessage } from "@/protoFleet/api/generated/errors/v1/errors_pb";
import type { MinerStateSnapshot } from "@/protoFleet/api/generated/fleetmanagement/v1/fleetmanagement_pb";
import { miners } from "@/protoFleet/features/fleetManagement/components/MinerList/stories/mocks";
import {
  allIssueMiners,
  allStatusMiners,
  errorMessages,
} from "@/protoFleet/features/fleetManagement/components/MinerList/stories/statusMocks";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

const meta: Meta<typeof MinerListComponent> = {
  title: "Proto Fleet/MinerList",
  component: MinerListComponent,
};

export default meta;
type Story = StoryObj<typeof MinerListComponent>;

const buildMinersRecord = (minerList: MinerStateSnapshot[]): Record<string, MinerStateSnapshot> =>
  Object.fromEntries(minerList.map((m) => [m.deviceIdentifier, m]));

const buildErrorsByDevice = (
  minerList: MinerStateSnapshot[],
  errors: ErrorMessage[],
): Record<string, ErrorMessage[]> => {
  const byDevice: Record<string, ErrorMessage[]> = {};
  for (const m of minerList) {
    byDevice[m.deviceIdentifier] = [];
  }
  for (const error of errors) {
    if (error.deviceIdentifier && byDevice[error.deviceIdentifier]) {
      byDevice[error.deviceIdentifier].push(error);
    }
  }
  return byDevice;
};

// Helper component to render MinerList with props derived from mock data
const MinerListWrapper = ({ minerList }: { minerList: MinerStateSnapshot[] }) => {
  const minerIds = minerList.map((miner) => miner.deviceIdentifier);
  const minersRecord = buildMinersRecord(minerList);
  const errorsByDevice = buildErrorsByDevice(minerList, errorMessages);

  return (
    <div>
      <div className="fixed right-4 bottom-4 z-30 phone:right-2 phone:bottom-2">
        <ToasterComponent />
      </div>
      <MinerListComponent
        title="Miners"
        minerIds={minerIds}
        miners={minersRecord}
        errorsByDevice={errorsByDevice}
        errorsLoaded={true}
        getActiveBatches={() => []}
        onAddMiners={action("onAddMiners")}
      />
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
      <MinerListComponent
        title="Miners"
        minerIds={[]}
        miners={{}}
        errorsByDevice={{}}
        errorsLoaded={true}
        getActiveBatches={() => []}
        totalMiners={0}
        onAddMiners={action("onAddMiners")}
      />
    </div>
  ),
};
