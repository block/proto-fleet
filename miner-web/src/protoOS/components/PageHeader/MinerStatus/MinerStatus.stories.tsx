import { ElementType } from "react";
import { MemoryRouter } from "react-router-dom";

import {
  getMockErrorList,
  mockErrorListProps,
  storyArgs,
  storyArgTypes,
} from "./constants";
import MinerStatusComponent from "./MinerStatus";

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
    <div className="w-96 flex justify-end">
      <MinerStatusComponent
        errors={loading ? undefined : mockErrorResponse}
        loading={loading}
      />
    </div>
  );
};

export default {
  title: "Components (protoOS)/Page Header/Miner Status",
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
