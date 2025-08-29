import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import MinerStatusModalComponent from "./MinerStatusModal";
import { statuses } from "@/shared/components/StatusCircle/";
const noErrors = {
  title: "All systems are operational",
  circle: statuses.normal,
  hasIssues: false,
  isSleeping: false,
  text: "Hashing",
  issues: {
    fans: [],
    hashboards: [],
    psus: [],
    controlBoard: [],
  },
};

const fanErrors = {
  title: "Fan not detected",
  hasIssues: true,
  circle: statuses.error,
  isSleeping: false,
  text: "Fan 2 issue",
  issues: {
    fans: [
      {
        title: "Fan 2",
        message: "Fan not detected",
      },
    ],
    hashboards: [],
    psus: [],
    controlBoard: [],
  },
};

const testStatuses = {
  noErrors,
  fanErrors,
};

type TestStatus = keyof typeof testStatuses;

export const MinerStatusModal = ({
  testStatus,
}: {
  testStatus: TestStatus;
}) => {
  return (
    <div>
      <MinerStatusModalComponent
        onDismiss={() => {}}
        status={testStatuses[testStatus]}
      />
    </div>
  );
};

MinerStatusModal.args = {
  testStatus: "noErrors",
};

MinerStatusModal.argTypes = {
  testStatus: {
    control: "select",
    options: Object.keys(testStatuses),
    description: "Select status to display",
  },
};

export default {
  title: "Shared/MinerStatusModal",
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
