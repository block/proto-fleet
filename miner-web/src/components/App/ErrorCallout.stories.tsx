import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import App from "components/App/App";
import {
  getMockErrorList,
  mockErrorListProps,
  storyArgs,
  storyArgTypes,
} from "components/PageHeader/MinerStatus/constants";

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
    <App
      title="Page title"
      apiErrors={mockErrorResponse}
      pendingErrors={loading}
    >
      Page content
    </App>
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
