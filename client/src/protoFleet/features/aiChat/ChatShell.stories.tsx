import { useEffect } from "react";
import type { Meta, StoryObj } from "@storybook/react";

import ChatFab from "./ChatFab";
import ChatPanel from "./ChatPanel";
import { useChatStore } from "./useChatStore";

type Scenario = "closed" | "empty" | "operationalReview" | "streaming";

interface ChatShellStoryProps {
  scenario: Scenario;
}

const seedScenario = (scenario: Scenario) => {
  const store = useChatStore.getState();
  store.clearMessages();
  store.resetStream();
  store.setStreaming(false);
  store.open();

  if (scenario === "closed") {
    store.close();
    return;
  }

  if (scenario === "empty") return;

  store.addMessage("user", "Which firmware updates should I prioritize today?");
  store.beginToolActivity("scan-firmware", "Checking firmware versions across the fleet");
  store.finishToolActivity("scan-firmware", true, "Found 42 miners behind the target firmware");
  store.addMessage(
    "assistant",
    "42 miners are behind the target firmware. Prioritize Building 2, Rack B, where stale firmware overlaps with a 6.8% hashrate drop.\n\n| Batch | Miners | Expected lift |\n| --- | ---: | ---: |\n| Building 2 / Rack B | 18 | 6.8% |\n| Building 1 / Rack D | 14 | 3.1% |\n| Yard units | 10 | 1.4% |",
  );

  if (scenario === "streaming") {
    store.beginToolActivity("compare-market", "Comparing power price, pool fees, and hashprice");
    store.setStreaming(true);
    store.appendStreamingContent("I would hold curtailment for 2 hours and keep high-efficiency racks online first.");
    return;
  }

  store.addToolConfirmation({
    id: "firmware-update-confirmation",
    toolCallId: "scan-firmware",
    title: "Review firmware update?",
    description: "Minerbot will prepare a staged update for the affected miners before any change is applied.",
    confirmLabel: "Review update",
    details: [
      { label: "Scope", value: "42 miners" },
      { label: "Batch size", value: "10 miners" },
      { label: "Maintenance window", value: "Today, 11:00 PM" },
    ],
  });
};

const ChatShellStory = ({ scenario }: ChatShellStoryProps) => {
  useEffect(() => {
    seedScenario(scenario);

    return () => {
      const store = useChatStore.getState();
      store.close();
      store.clearMessages();
      store.resetStream();
      store.setStreaming(false);
    };
  }, [scenario]);

  return (
    <div className="min-h-screen bg-surface-10 p-8 text-text-primary">
      <div className="mx-auto grid max-w-[1120px] gap-5">
        <header className="flex items-center justify-between">
          <div>
            <h1 className="text-heading-300">Fleet dashboard</h1>
            <p className="mt-1 text-300 text-text-primary-50">Cedar Creek site</p>
          </div>
          <div className="rounded-full bg-intent-success-10 px-3 py-1 text-emphasis-200 text-text-success">
            94% online
          </div>
        </header>

        <div className="grid gap-4 tablet:grid-cols-3">
          {[
            ["Hashrate", "812 PH/s", "+2.8%"],
            ["Power draw", "26.4 MW", "-1.1%"],
            ["Revenue pace", "$43.2K", "+4.5%"],
          ].map(([label, value, trend]) => (
            <section key={label} className="rounded-lg border border-border-5 bg-surface-base p-4">
              <p className="text-200 text-text-primary-50">{label}</p>
              <div className="mt-2 flex items-end justify-between">
                <p className="text-heading-300">{value}</p>
                <p className="text-emphasis-200 text-text-success">{trend}</p>
              </div>
            </section>
          ))}
        </div>

        <section className="rounded-lg border border-border-5 bg-surface-base p-5">
          <div className="flex items-center justify-between">
            <h2 className="text-heading-200">Rack health</h2>
            <p className="text-200 text-text-primary-50">Live operating view</p>
          </div>
          <div className="mt-5 grid grid-cols-12 gap-2">
            {Array.from({ length: 72 }, (_, index) => {
              const stateClass =
                index % 29 === 0
                  ? "bg-intent-critical-fill"
                  : index % 11 === 0
                    ? "bg-intent-warning-fill"
                    : "bg-intent-success-fill";
              return <div key={index} className={`h-8 rounded-sm ${stateClass}`} />;
            })}
          </div>
        </section>
      </div>

      <ChatFab />
      <ChatPanel />
    </div>
  );
};

const meta = {
  title: "Proto Fleet/AI Chat/Chat Shell",
  component: ChatShellStory,
  args: {
    scenario: "operationalReview",
  },
  argTypes: {
    scenario: {
      control: "select",
      options: ["closed", "empty", "operationalReview", "streaming"],
    },
  },
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof ChatShellStory>;

export default meta;
type Story = StoryObj<typeof meta>;

export const OperationalReview: Story = {
  args: {
    scenario: "operationalReview",
  },
};

export const Empty: Story = {
  args: {
    scenario: "empty",
  },
};

export const ClosedFab: Story = {
  args: {
    scenario: "closed",
  },
};

export const Streaming: Story = {
  args: {
    scenario: "streaming",
  },
};
