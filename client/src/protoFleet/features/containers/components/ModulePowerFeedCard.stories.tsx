import { create, toJson } from "@bufbuild/protobuf";
import { timestampFromMs, timestampNow } from "@bufbuild/protobuf/wkt";
import type { Meta, StoryObj } from "@storybook/react";
import { spyOn, userEvent, within } from "storybook/test";
import ModulePowerFeedCard from "./ModulePowerFeedCard";
import {
  ControlModulePowerFeedResponseSchema,
  GetEquipmentCommandResultResponseSchema,
  GetModulePowerFeedResponseSchema,
  PowerFeedAction,
  PowerFeedMappingConfidence,
  PowerFeedState,
} from "@/protoFleet/api/generated/containers/v1/containers_pb";

type Feed = "enabled" | "overridden_off" | "pending" | "cycling";
type Outcome = "confirmed" | "controller_refused" | "not_queued" | "unknown" | "waiting";
type Args = { feed: Feed; outcome: Outcome };

const label = { turn_on: "Turn on feed", turn_off: "Turn off feed", cycle: "Cycle feed" } as const;

function feedResponse(feed: Feed, restoreAfter: number) {
  const cycling = feed === "cycling";
  const off = feed === "overridden_off" || cycling;
  const overridden = feed === "overridden_off";
  // Mirrors the server: actions follow the feed's state; a reason disables them all.
  const actions = [
    off ? PowerFeedAction.TURN_ON : PowerFeedAction.TURN_OFF,
    ...(off ? [] : [PowerFeedAction.CYCLE]),
    ...(overridden ? [PowerFeedAction.CLEAR_OVERRIDE] : []),
  ];
  const unavailableReason =
    feed === "pending"
      ? "A power feed command is already pending for this container."
      : cycling
        ? "Controls are off while a power recovery cycle is in progress."
        : "";
  return create(GetModulePowerFeedResponseSchema, {
    moduleId: 91n,
    deviceIdentifier: "ct1",
    containerName: "CT1 North",
    tank: 1,
    module: 5,
    observedAt: timestampNow(),
    mapping: { confidence: PowerFeedMappingConfidence.CONFIGURED, feedId: "pdu-1-outlet-05" },
    feed: {
      id: "pdu-1-outlet-05",
      state: off ? PowerFeedState.DISABLED : PowerFeedState.ENABLED,
      active: !off,
      faulted: false,
      overridden,
      voltageV: off ? 0 : 480.2,
      currentA: off ? 0 : 21.9,
      powerW: off ? 0 : 18_240,
    },
    recoveryCycleKnown: true,
    recoveryCycle: cycling
      ? {
          id: "cycle-1",
          phase: "holding",
          outletIds: ["pdu-1-outlet-05"],
          restoreAfter: timestampFromMs(restoreAfter),
        }
      : undefined,
    control: { actions, unavailableReason, cycleHoldSeconds: 30 },
  });
}

function commandResult(outcome: Outcome) {
  const failure = {
    controller_refused:
      "failed_precondition: container power feed: controller refused to turn off power feed pdu-1-outlet-05: feed is locked by a safety interlock",
    unknown: "failed_precondition: module power feed outcome unconfirmed",
  }[outcome as "controller_refused" | "unknown"];
  return create(GetEquipmentCommandResultResponseSchema, {
    result: {
      batchIdentifier: "batch-1",
      commandType: "ControlPowerFeed",
      status: outcome === "waiting" ? "processing" : "finished",
      totalCount: 1,
      successCount: outcome === "confirmed" ? 1 : 0,
      failureCount: failure ? 1 : 0,
      deviceResults: failure ? [{ deviceIdentifier: "ct1", errorMessage: failure }] : [],
    },
  });
}

/** Opens the card's menu, picks the action and confirms it. */
const confirmAction =
  (action: keyof typeof label) =>
  async ({ canvasElement }: { canvasElement: HTMLElement }) => {
    const page = within(canvasElement.ownerDocument.body);
    await userEvent.click(await page.findByTestId("power-feed-actions-trigger"));
    await userEvent.click(await page.findByText(label[action]));
    const dialog = within(await page.findByTestId("power-feed-action-dialog"));
    await userEvent.click(dialog.getByRole("button", { name: label[action] }));
  };

// The arguments only drive the mocked responses below.
function ModulePowerFeedPreview(_args: Args) {
  return (
    <div className="max-w-6xl bg-surface-base p-6">
      <ModulePowerFeedCard moduleId={91n} enabled />
    </div>
  );
}

const meta = {
  title: "Proto Fleet/Containers/Module power feed",
  component: ModulePowerFeedPreview,
  parameters: { layout: "fullscreen" },
  args: { feed: "enabled", outcome: "confirmed" },
  beforeEach: ({ args }) => {
    let feed = args.feed;
    const restoreAfter = Date.now() + 25_000;
    const original = window.fetch.bind(window);
    const json = (body: unknown, status = 200) =>
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
    const mock = spyOn(window, "fetch").mockImplementation(async (input, init) => {
      const request = new Request(input, init);
      if (request.url.endsWith("/GetModulePowerFeed"))
        return json(toJson(GetModulePowerFeedResponseSchema, feedResponse(feed, restoreAfter)));
      if (request.url.endsWith("/ControlModulePowerFeed")) {
        if (args.outcome === "not_queued")
          return json(
            {
              code: "failed_precondition",
              message: "The module moved or was replaced after you confirmed it. Refresh before retrying.",
            },
            400,
          );
        const body = await request.json();
        if (args.outcome === "confirmed" && body.action === "POWER_FEED_ACTION_CYCLE") feed = "cycling";
        return json(
          toJson(
            ControlModulePowerFeedResponseSchema,
            create(ControlModulePowerFeedResponseSchema, { deviceIdentifier: "ct1", batchIdentifier: "batch-1" }),
          ),
        );
      }
      if (request.url.endsWith("/GetEquipmentCommandResult"))
        return json(toJson(GetEquipmentCommandResultResponseSchema, commandResult(args.outcome)));
      return original(input, init);
    });
    return () => mock.mockRestore();
  },
} satisfies Meta<typeof ModulePowerFeedPreview>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Controls: Story = {};
export const OverriddenOff: Story = { args: { feed: "overridden_off" } };
export const CommandPending: Story = { args: { feed: "pending" } };
export const CycleInProgress: Story = { args: { feed: "cycling" } };
export const WaitingForResult: Story = { args: { outcome: "waiting" }, play: confirmAction("turn_off") };
export const CycleAccepted: Story = { play: confirmAction("cycle") };
export const ControllerRefused: Story = { args: { outcome: "controller_refused" }, play: confirmAction("turn_off") };
export const NotQueued: Story = { args: { outcome: "not_queued" }, play: confirmAction("turn_off") };
export const OutcomeUnknown: Story = {
  args: { feed: "overridden_off", outcome: "unknown" },
  play: confirmAction("turn_on"),
};
