import { type ReactNode, useEffect, useState } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import type { Meta, StoryObj } from "@storybook/react";

import FleetDown from "./FleetDown";
import { onboardingClient } from "@/protoFleet/api/clients";
import { createRefCountedStoryMock } from "@/shared/stories/createRefCountedStoryMock";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };

const mutableOnboardingClient = onboardingClient as MutableClient<typeof onboardingClient>;

const MockedFleetDownApi = ({ children }: { children: ReactNode }) => {
  // Defer rendering FleetDown until the mock is committed. Otherwise its
  // child usePoll effect fires before this decorator's effect installs the
  // mock, the real getFleetInitStatus throws a non-ConnectError, and
  // redirectFromFleetDown navigates the iframe to "/" — rendering Storybook
  // nested inside its own preview.
  const [installed, setInstalled] = useState(false);

  useEffect(() => {
    const cleanup = installMockedFleetDownApi();
    // eslint-disable-next-line react-hooks/set-state-in-effect -- intentional: gate child render until mock is installed so usePoll never sees the real client
    setInstalled(true);
    return cleanup;
  }, []);

  if (!installed) return null;
  return <>{children}</>;
};

const installMockedFleetDownApi = createRefCountedStoryMock(() => {
  const originalGetFleetInitStatus = mutableOnboardingClient.getFleetInitStatus;

  mutableOnboardingClient.getFleetInitStatus = async () => {
    throw new ConnectError("Backend unavailable", Code.Unavailable);
  };

  return () => {
    mutableOnboardingClient.getFleetInitStatus = originalGetFleetInitStatus;
  };
});

const withMockedFleetDownApi = (Story: () => ReactNode) => (
  <MockedFleetDownApi>
    <Story />
  </MockedFleetDownApi>
);

const meta = {
  title: "Proto Fleet/FleetDown",
  component: FleetDown,
  parameters: {
    layout: "fullscreen",
  },
  decorators: [withMockedFleetDownApi],
  tags: ["autodocs"],
} satisfies Meta<typeof FleetDown>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  parameters: {
    docs: {
      description: {
        story: "Error page displayed when the backend server is completely down.",
      },
    },
  },
};
