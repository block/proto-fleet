import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";
import MinerStatusWidget from "./MinerStatusWidget";
import { statuses } from "@/shared/components/StatusCircle/";

const sleeping = {
  circle: statuses.inactive,
  summary: "Sleeping",
  title: "Miner is sleeping",
  isSleeping: true,
  hasIssues: false,
  issues: {
    hashboards: [],
    psus: [],
    fans: [],
    controlBoard: [],
  },
};

const noErrors = {
  circle: statuses.normal,
  summary: "Hashing",
  title: "All systems are operational",
  hasIssues: false,
  isSleeping: false,
  issues: {
    hashboards: [],
    psus: [],
    fans: [],
    controlBoard: [],
  },
};

const fanError = {
  circle: statuses.error,
  title: "Fan 2 not detected",
  summary: "Fan 2 Issue",
  hasIssues: true,
  isSleeping: false,
  issues: {
    hashboards: [],
    psus: [],
    fans: [
      {
        title: "Fan 2 not spinning",
        message: "Fan 2 not spinning",
      },
    ],
    controlBoard: [],
  },
};

const multipleFanErrors = {
  circle: statuses.error,
  summary: "Multiple Fan Issues",
  title: "Multiple Fan Issues",
  hasIssues: true,
  isSleeping: false,
  issues: {
    hashboards: [],
    psus: [],
    fans: [
      {
        message: "Fan 1 not spinning",
      },
      {
        message: "Fan 2 not spinning",
      },
    ],
    controlBoard: [],
  },
};

const multipleComponentErrors = {
  circle: statuses.error,
  summary: "Multiple Issues",
  title: "Multiple Issues",
  hasIssues: true,
  isSleeping: false,
  issues: {
    hashboards: [
      {
        message: "Hashboard 1 overheating",
        details: "details about Hashboard error",
      },
      {
        message: "Hashboard 2 disconnected",
        details: "details about Hashboard error",
      },
    ],
    psus: [],
    fans: [
      {
        message: "Fan 1 not spinning",
        details: "details about Fan error",
      },
      {
        message: "Fan 2 not spinning",
        details: "details about Fan error",
      },
    ],
    controlBoard: [],
  },
};

const testStatuses = {
  noErrors,
  sleeping,
  fanError,
  multipleFanErrors,
  multipleComponentErrors,
};

type TestStatus = keyof typeof testStatuses;

// For Storybook, we use the MinerStatusWidget directly to bypass store dependencies
export const MinerStatus = ({ testStatus }: { testStatus: TestStatus }) => {
  const status = testStatuses[testStatus];

  return (
    <div className="mx-auto flex w-96 justify-end gap-2">
      <MinerStatusWidget circle={status.circle} summary={status.summary} onClick={() => {}} />
    </div>
  );
};

MinerStatus.args = {
  testStatus: "noErrors",
};

MinerStatus.argTypes = {
  testStatus: {
    control: "select",
    options: Object.keys(testStatuses),
    description: "Select status to display",
  },
};

export default {
  title: "Proto OS/Page Header/Miner Status",
  parameters: {
    withRouter: false,
  },
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
