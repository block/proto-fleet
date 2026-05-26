import { type ReactElement } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";

import EnergyPage from "@/protoFleet/features/energy/EnergyPage";
import { mockActiveEvent, mockHistoryEvents } from "@/protoFleet/features/energy/fixtures";
import type {
  CurtailmentApi,
  CurtailmentEventState,
  CurtailmentHistoryEvent,
} from "@/protoFleet/features/energy/types";

const meta: Meta<typeof EnergyPage> = {
  title: "Proto Fleet/Energy",
  component: EnergyPage,
  parameters: {
    layout: "fullscreen",
  },
};

export default meta;

type Story = StoryObj<typeof EnergyPage>;

const restoredHistoryEvent: CurtailmentHistoryEvent = {
  ...mockHistoryEvents[0],
  state: "completed",
  endedAt: "2026-04-30T14:08:00-04:00",
};

function getResolvedHistoryEvents(
  activeEvent: CurtailmentApi["activeEvent"],
  events?: CurtailmentHistoryEvent[],
): CurtailmentHistoryEvent[] {
  if (events) {
    return events;
  }

  if (activeEvent) {
    return mockHistoryEvents;
  }

  return [restoredHistoryEvent, ...mockHistoryEvents.slice(1)];
}

function filterHistoryEvents(
  events: CurtailmentHistoryEvent[],
  stateFilters?: CurtailmentEventState[],
): CurtailmentHistoryEvent[] {
  if (!stateFilters || stateFilters.length === 0) {
    return events;
  }

  return events.filter((event) => stateFilters.includes(event.state));
}

function createMockApi({
  activeEvent,
  events,
  isLoading = false,
}: {
  activeEvent?: CurtailmentApi["activeEvent"];
  events?: CurtailmentHistoryEvent[];
  isLoading?: boolean;
}): CurtailmentApi {
  const resolvedEvents = getResolvedHistoryEvents(activeEvent, events);

  return {
    activeEvent,
    events: resolvedEvents,
    isLoading,
    refreshCurtailment: async (stateFilters) => ({
      activeEvent,
      events: filterHistoryEvents(resolvedEvents, stateFilters),
    }),
    startCurtailment: async () => ({ event: activeEvent ?? mockActiveEvent }),
    stopCurtailment: async () => ({ event: activeEvent ? { ...activeEvent, state: "restoring" } : mockActiveEvent }),
    updateCurtailmentEvent: async () => ({ event: activeEvent ?? mockActiveEvent }),
  };
}

function renderPage(api: CurtailmentApi): ReactElement {
  return (
    <div className="min-h-screen bg-surface-base p-10">
      <EnergyPage api={api} />
    </div>
  );
}

export const ActiveCurtailment: Story = {
  render: () => renderPage(createMockApi({ activeEvent: mockActiveEvent })),
};

export const HistoryOnly: Story = {
  render: () => renderPage(createMockApi({ activeEvent: undefined })),
};
