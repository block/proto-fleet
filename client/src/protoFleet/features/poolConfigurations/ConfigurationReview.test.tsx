import { act, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import { ConfigurationReview } from "./ConfigurationReview";
import { ConfigurationSchema } from "@/protoFleet/api/generated/poolconfigurations/v1/poolconfigurations_pb";
import { useFleetStore } from "@/protoFleet/store";

const mocks = vi.hoisted(() => ({ confirmReview: vi.fn(), track: vi.fn() }));
vi.mock("./api", () => ({ usePoolConfigurationClient: () => ({ client: mocks, accountId: "" }) }));
vi.mock("./assignmentProgress", () => ({ trackPoolOperation: mocks.track }));
const configuration = create(ConfigurationSchema, { configurationId: "config", name: "Production" });
beforeEach(() => {
  vi.clearAllMocks();
});

it.each(["session", "unmount"])("ignores an accepted confirmation after %s ends", async (boundary) => {
  let reply!: (saved: { configuration: typeof configuration; operationId: string }) => void;
  mocks.confirmReview.mockReturnValueOnce(
    new Promise((resolve) => {
      reply = resolve;
    }),
  );
  const onApplied = vi.fn();
  const view = render(
    <ConfigurationReview
      review={{ reviewId: "review", configuration, affectedMiners: 1n, affectedHashboards: 0n }}
      initialPassword="secret"
      onApplied={onApplied}
      onDismiss={vi.fn()}
    />,
  );
  await userEvent.setup().click(screen.getByRole("button", { name: "Assign" }));
  if (boundary === "unmount") view.unmount();
  else
    act(() =>
      useFleetStore.setState((state) => ({
        auth: { ...state.auth, sessionGeneration: state.auth.sessionGeneration + 1 },
      })),
    );
  await act(async () => reply({ configuration, operationId: "accepted-before-close" }));
  expect(mocks.track).not.toHaveBeenCalled();
  expect(onApplied).not.toHaveBeenCalled();
});
