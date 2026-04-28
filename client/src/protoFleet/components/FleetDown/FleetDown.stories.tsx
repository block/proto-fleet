import { type ReactNode, useEffect, useRef } from "react";
import { Code, ConnectError } from "@connectrpc/connect";
import type { Meta, StoryObj } from "@storybook/react";

import FleetDown from "./FleetDown";
import { onboardingClient } from "@/protoFleet/api/clients";
import { createRefCountedStoryMock } from "@/shared/stories/createRefCountedStoryMock";

type MutableClient<T> = { -readonly [K in keyof T]: T[K] };

const mutableOnboardingClient = onboardingClient as MutableClient<typeof onboardingClient>;

const MockedFleetDownApi = ({ children }: { children: ReactNode }) => {
  const cleanupRef = useRef<(() => void) | null>(null);
  // Install during render so the mock is in place before FleetDown's child
  // effects fire. Without this, usePoll triggers a real getFleetInitStatus
  // call before the decorator's effect runs, the non-ConnectError causes
  // redirectFromFleetDown to navigate the iframe to "/", and Storybook
  // renders nested inside its own preview.
  if (cleanupRef.current === null) {
    cleanupRef.current = installMockedFleetDownApi();
  }

  useEffect(() => {
    return () => {
      cleanupRef.current?.();
      cleanupRef.current = null;
    };
  }, []);

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
