import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { CurtailmentAutomationsContent } from "@/protoFleet/features/settings/components/Curtailment/CurtailmentAutomations";
import type {
  AutomationRule,
  CurtailmentSource,
  ResponseProfile,
} from "@/protoFleet/features/settings/components/Curtailment/types";

const testSources: CurtailmentSource[] = [
  {
    id: "source-alpha",
    name: "Site Alpha MaestroOS",
    triggerType: "MQTT",
    brokerHosts: ["maestro-primary.test", "maestro-backup.test"],
    port: 11883,
    topic: "curtailment/site-alpha/target",
    protocol: "MQTT",
    qos: 1,
    username: "site-alpha",
    lastTarget: "0",
    lastSeen: "38 seconds ago",
    health: "connected",
    enabled: true,
    stalenessThresholdSec: 240,
  },
  {
    id: "source-beta",
    name: "Site Beta MaestroOS",
    triggerType: "MQTT",
    brokerHosts: ["site-beta-primary.test", "site-beta-backup.test"],
    port: 11884,
    topic: "curtailment/site-beta/target",
    protocol: "MQTT",
    qos: 1,
    username: "site-beta",
    lastTarget: "100",
    lastSeen: "24 seconds ago",
    health: "connected",
    enabled: true,
    stalenessThresholdSec: 240,
  },
];

const testResponseProfiles: ResponseProfile[] = [
  {
    id: "standard-shed",
    name: "Standard shed",
    targetSummary: "50% reduction",
    scope: "Whole fleet",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore in batches",
    deadlineSummary: "Within 15 min",
  },
  {
    id: "partial-reduction",
    name: "Partial reduction",
    targetSummary: "2,000 kW target",
    scope: "Whole fleet",
    selectionStrategy: "Least efficient first",
    restoreBehavior: "Restore immediately",
    deadlineSummary: "Within 10 min",
  },
];

const testAutomationRules: AutomationRule[] = [
  {
    id: "ercot-ers-obligation",
    priority: 1,
    name: "ERCOT ERS obligation",
    conditionType: "mqttTriggerTargetOff",
    conditionSummary: "ERCOT ERS (Emergency Response Service)",
    sourceId: "source-alpha",
    responseProfileId: "standard-shed",
    enabled: true,
  },
];

function renderAutomations(): void {
  render(<CurtailmentAutomationsContent sources={testSources} responseProfiles={testResponseProfiles} />);
}

function getAutomationRow(ruleName: string): HTMLTableRowElement {
  const row = screen.getByText(ruleName).closest("tr");
  expect(row).not.toBeNull();
  return row as HTMLTableRowElement;
}

describe("CurtailmentAutomationsContent", () => {
  it("renders the automations table with the info popover text", () => {
    render(
      <CurtailmentAutomationsContent
        initialAutomationRules={testAutomationRules}
        sources={testSources}
        responseProfiles={testResponseProfiles}
      />,
    );

    expect(screen.getByText("Automations")).toBeVisible();
    expect(screen.getByRole("button", { name: "Create automation" })).toBeEnabled();
    expect(screen.getByRole("columnheader", { name: "Name" })).toHaveClass("text-text-primary");
    expect(screen.getByRole("columnheader", { name: "Condition" })).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "Response profile" })).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "Enabled" })).toBeInTheDocument();
    expect(screen.getByText("ERCOT ERS obligation")).toBeVisible();
    expect(screen.getByText("ERCOT ERS (Emergency Response Service)")).toBeVisible();
    expect(screen.getByText("Standard shed")).toBeVisible();

    fireEvent.click(screen.getByRole("button", { name: "About automations" }));

    expect(screen.getByTestId("curtailment-automations-info-popover")).toHaveTextContent(
      "Conditions that automatically trigger a response profile.",
    );
  });

  it("creates an automation from the selected source trigger and response profile", async () => {
    renderAutomations();

    fireEvent.click(screen.getByRole("button", { name: "Create automation" }));

    expect(screen.getByTestId("curtailment-automation-modal")).toHaveTextContent("Create automation");
    expect(screen.getByText("Conditions that automatically trigger a response profile.")).toBeInTheDocument();
    expect(screen.getByLabelText("Rule name")).toHaveValue("");
    expect(screen.getByTestId("automation-trigger-source-select")).toHaveTextContent("Site Alpha MaestroOS");
    expect(screen.getByLabelText("Grid signal")).toHaveValue(0);
    expect(screen.getByLabelText("Grid signal")).toHaveAttribute("readonly");
    expect(
      screen.getByText(
        "When the signal changes to 100, your selected response profile will begin the restore process.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByTestId("automation-response-profile-select")).toHaveTextContent("Standard shed");
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(saveButton).toBeEnabled();

    fireEvent.click(saveButton);

    await waitFor(() => expect(screen.getByText("Enter a rule name.")).toBeVisible());
    expect(screen.queryByText("High LMP spike")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Rule name"), { target: { value: "High LMP spike" } });
    fireEvent.keyDown(screen.getByLabelText("Rule name"), { key: "Enter", code: "Enter" });

    await waitFor(() => expect(screen.queryByTestId("curtailment-automation-modal")).not.toBeInTheDocument());

    const row = getAutomationRow("High LMP spike");
    expect(within(row).getByText("Site Alpha MaestroOS grid signal changes to 0")).toBeVisible();
    expect(within(row).getByText("Standard shed")).toBeVisible();
  });

  it("includes facility-fan response profiles in automation choices", () => {
    const facilityFanProfile: ResponseProfile = {
      ...testResponseProfiles[0],
      id: "facility-fan-shed",
      name: "Facility fan shed",
      formValues: { facilityFanDeviceIds: ["31"] } as NonNullable<ResponseProfile["formValues"]>,
    };
    render(
      <CurtailmentAutomationsContent
        sources={testSources}
        responseProfiles={[facilityFanProfile, ...testResponseProfiles]}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Create automation" }));
    const responseProfileSelect = screen.getByTestId("automation-response-profile-select");
    expect(responseProfileSelect).toHaveTextContent("Facility fan shed");
  });

  it("edits and deletes automation rows from the row click modal", async () => {
    render(
      <CurtailmentAutomationsContent
        initialAutomationRules={testAutomationRules}
        sources={testSources}
        responseProfiles={testResponseProfiles}
      />,
    );

    fireEvent.click(getAutomationRow("ERCOT ERS obligation"));

    expect(screen.getByTestId("curtailment-automation-modal")).toHaveTextContent("Edit automation");
    expect(screen.getByLabelText("Rule name")).toHaveValue("ERCOT ERS obligation");
    expect(screen.getByTestId("automation-trigger-source-select")).toHaveTextContent("Site Alpha MaestroOS");
    expect(screen.getByTestId("automation-response-profile-select")).toHaveTextContent("Standard shed");
    const deleteButton = screen.getByRole("button", { name: "Delete" });
    const saveButton = screen.getByRole("button", { name: "Save" });
    expect(deleteButton.compareDocumentPosition(saveButton)).toBe(Node.DOCUMENT_POSITION_FOLLOWING);

    fireEvent.change(screen.getByLabelText("Rule name"), { target: { value: "ERCOT ERS updated" } });
    fireEvent.click(saveButton);

    await waitFor(() => expect(screen.queryByTestId("curtailment-automation-modal")).not.toBeInTheDocument());
    // The row list re-renders with the new name a tick after the modal closes;
    // wait for it before the synchronous row lookup to avoid a load-dependent race.
    await screen.findByText("ERCOT ERS updated");
    const updatedRow = getAutomationRow("ERCOT ERS updated");
    expect(within(updatedRow).getByText("ERCOT ERS (Emergency Response Service)")).toBeVisible();
    expect(screen.queryByText("ERCOT ERS obligation")).not.toBeInTheDocument();

    fireEvent.click(updatedRow);
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(screen.queryByTestId("curtailment-automation-modal")).not.toBeInTheDocument());
    expect(screen.queryByText("ERCOT ERS updated")).not.toBeInTheDocument();
    expect(screen.getByText("No automations configured")).toBeVisible();
  });

  it("toggles automation rows without exposing reorder handles", () => {
    render(
      <CurtailmentAutomationsContent
        initialAutomationRules={testAutomationRules}
        sources={testSources}
        responseProfiles={testResponseProfiles}
      />,
    );

    const row = getAutomationRow("ERCOT ERS obligation");
    expect(within(row).getByTestId("name").firstElementChild).not.toHaveClass("opacity-50");
    expect(row.querySelector("[data-testid='reorder-handle']")).not.toBeInTheDocument();

    const toggle = row.querySelector("input[type='checkbox']");
    expect(toggle).not.toBeNull();
    fireEvent.click(toggle as HTMLInputElement);

    expect(screen.queryByTestId("curtailment-automation-modal")).not.toBeInTheDocument();
    expect(within(getAutomationRow("ERCOT ERS obligation")).getByTestId("name").firstElementChild).toHaveClass(
      "opacity-50",
    );
  });
});
