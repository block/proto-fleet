import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "@/protoOS/components/App/App";
import {
  getMockErrorList,
  mockErrorListProps,
  storyArgs,
  storyArgTypes,
} from "@/protoOS/components/PageHeader/MinerStatus/constants";

import { MinerStatusProvider } from "@/protoOS/contexts/MinerStatusContext";

interface MinerStatusProps extends mockErrorListProps {
  loading: boolean;
}

export const MinerStatus = ({
  loading,
  hashboardStatus,
  asicStatus,
  fanStatus,
  hashboardErrorCode,
  asicErrorCode,
  fanErrorCode,
}: MinerStatusProps) => {
  const mockErrorResponse = getMockErrorList({
    hashboardStatus,
    asicStatus,
    fanStatus,
    hashboardErrorCode,
    asicErrorCode,
    fanErrorCode,
  });

  return (
    <MinerStatusProvider
      apiErrors={loading ? undefined : mockErrorResponse}
      pendingErrors={loading}
    >
      <App title="Page title" pendingSystemInfo={false}>
        Page content
      </App>{" "}
    </MinerStatusProvider>
  );
};

export default {
  title: "Pages/App/Miner Status",
  args: storyArgs,
  argTypes: storyArgTypes,
  decorators: [
    (Story: ElementType) => (
      <MemoryRouter>
        <Story />
      </MemoryRouter>
    ),
  ],
};
