import { render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { create } from "@bufbuild/protobuf";
import userEvent from "@testing-library/user-event";

import ReportContent from "./ReportContent";
import { AccountReportSchema } from "@/protoFleet/api/generated/hosted/v1/reporting_pb";
import { reportTime } from "@/protoFleet/features/hostedAccounts/reportFormat";

beforeEach(() => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 40));
});
afterEach(() => vi.restoreAllMocks());

const report = () =>
  create(AccountReportSchema, {
    status: "issued",
    number: 2n,
    correctionOf: "original",
    period: { kind: "monthly", timezone: "UTC" },
    exception: "Reviewed missing source evidence",
    result: {
      complete: false,
      metrics: { metered_energy_kwh: { value: "125.5", unit: "kWh", status: "provisional" } },
      containers: [
        {
          siteId: 1n,
          containerId: 2n,
          metrics: {
            hashrate_performance_hs: { value: "167000000000000", unit: "H/s", status: "provisional" },
            metered_energy_kwh: {
              value: "75.5",
              unit: "kWh",
              status: "provisional",
              causes: ["estimated_meter_energy"],
              method: "interval_sum",
              sources: ["reviewed-meter-1"],
            },
            monthly_uptime: { status: "unavailable", causes: ["missing_G1", "missing_G3"] },
          },
        },
        {
          siteId: 1n,
          containerId: 3n,
          metrics: {
            metered_energy_kwh: { value: "50", unit: "kWh", status: "authoritative" },
          },
        },
      ],
      orders: [
        {
          orderId: "ORDER-01",
          metrics: { metered_energy_kwh: { value: "125.5", unit: "kWh", status: "provisional" } },
        },
      ],
      interruptions: [{ id: "outage", summary: "Cooling interruption", open: false }],
    },
  });

it("shows one row per container with measures across columns and no version chrome", () => {
  render(<ReportContent report={report()} />);
  expect(screen.getByText("Published")).toBeInTheDocument();
  expect(screen.queryByText(/Version \d/)).not.toBeInTheDocument();
  expect(screen.getByText(/Data is incomplete\./)).toBeInTheDocument();
  expect(screen.getByText("Reviewed missing source evidence")).toBeInTheDocument();
  const table = screen.getByRole("table");
  const row = within(table).getByText("Site 1 · Container 2").closest("tr")!;
  expect(within(row).getByText("167.0 TH/s")).toBeInTheDocument();
  expect(within(row).getByText("75.5 kWh")).toBeInTheDocument();
  expect(within(row).getAllByText("Provisional")).toHaveLength(2);
  expect(within(table).getByText("Site 1 · Container 3")).toBeInTheDocument();
  expect(within(table).queryByText("125.5 kWh")).not.toBeInTheDocument();
  expect(within(table).getAllByRole("row")).toHaveLength(3);
});

it("switches row grouping without combining alternative breakdowns", async () => {
  const user = userEvent.setup();
  render(<ReportContent report={report()} />);
  await user.click(screen.getByRole("button", { name: "Row grouping" }));
  await user.click(screen.getByRole("option", { name: "Orders" }));
  const orders = screen.getByRole("table");
  expect(within(orders).getByRole("columnheader", { name: "Order ref" })).toBeInTheDocument();
  expect(within(orders).getByText("ORDER-01")).toBeInTheDocument();
  expect(within(orders).getByText("125.5 kWh")).toBeInTheDocument();
  expect(within(orders).queryByText("Site 1 · Container 2")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Row grouping" }));
  await user.click(screen.getByRole("option", { name: "Account total" }));
  expect(within(screen.getByRole("table")).getAllByRole("row")).toHaveLength(2);
});

it("filters a measure's evidence and opens its qualifications and sources from the row", async () => {
  const user = userEvent.setup();
  render(<ReportContent report={report()} />);
  await user.click(screen.getByRole("button", { name: "Evidence for Metered energy · Site 1 · Container 2" }));
  const table = screen.getByRole("table");
  expect(
    within(table)
      .getAllByRole("columnheader")
      .map((header) => header.textContent),
  ).toEqual(["Container", "Measure", "Value", "Quality"]);
  expect(within(table).queryByText("Energy includes estimated meter readings.")).not.toBeInTheDocument();
  expect(within(table).queryByText("reviewed-meter-1")).not.toBeInTheDocument();
  expect(within(table).queryByText("Site 1 · Container 3")).not.toBeInTheDocument();
  await user.click(within(table).getByText("75.5 kWh"));
  const details = screen.getByRole("dialog", { name: "Evidence details" });
  expect(within(details).getByText("Site 1 · Container 2")).toBeInTheDocument();
  expect(within(details).getByText("Energy includes estimated meter readings.")).toBeInTheDocument();
  expect(within(details).getByText("Interval sum")).toBeInTheDocument();
  expect(within(details).getByText("reviewed-meter-1")).toBeInTheDocument();
  await user.click(within(details).getByRole("button", { name: "Close dialog" }));
  expect(within(table).getAllByRole("row")).toHaveLength(2);
  await user.click(screen.getByRole("button", { name: "Clear evidence filter" }));
  const unavailable = within(screen.getByRole("table")).getByText("Monthly uptime").closest("tr")!;
  expect(within(unavailable).getByText("—")).toBeInTheDocument();
  expect(within(unavailable).getByText("Unavailable")).toBeInTheDocument();
  unavailable.focus();
  await user.keyboard("{Enter}");
  const unavailableDetails = screen.getByRole("dialog", { name: "Evidence details" });
  expect(
    within(unavailableDetails).getByText(/Contract definitions and calculation methods have not been approved\./),
  ).toBeInTheDocument();
  expect(within(unavailableDetails).getByText("Method").nextElementSibling).toHaveTextContent("—");
  expect(within(unavailableDetails).getByText("Sources").nextElementSibling).toHaveTextContent("—");
  expect(screen.queryByText(/Missing G[1-5]/)).not.toBeInTheDocument();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(unavailable).toHaveFocus();
});

it("updates the open evidence details when report data is refreshed", async () => {
  const user = userEvent.setup();
  const value = report();
  const { rerender } = render(<ReportContent report={value} />);
  await user.click(screen.getByRole("button", { name: "Evidence for Metered energy · Site 1 · Container 2" }));
  await user.click(screen.getByText("75.5 kWh"));
  const details = screen.getByRole("dialog", { name: "Evidence details" });
  value.result!.containers[0].metrics.metered_energy_kwh.sources = ["Updated independently reviewed source"];
  rerender(<ReportContent report={value} />);
  expect(within(details).getByText("Updated independently reviewed source")).toBeInTheDocument();
  expect(within(details).queryByText("reviewed-meter-1")).not.toBeInTheDocument();
  delete value.result!.containers[0].metrics.metered_energy_kwh;
  rerender(<ReportContent report={value} />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByText("No evidence is included for this selection.")).toBeInTheDocument();
});

it("shows only the selected sheet's grid", async () => {
  const user = userEvent.setup();
  render(<ReportContent report={report()} />);
  await user.click(screen.getByRole("button", { name: "Events" }));
  expect(screen.getAllByRole("table")).toHaveLength(1);
  expect(within(screen.getByRole("table")).getByText("Cooling interruption")).toBeInTheDocument();
  expect(screen.queryByText("75.5 kWh")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Repairs" }));
  expect(screen.getByText("No repairs are recorded for this report.")).toBeInTheDocument();
  expect(screen.queryByText("Cooling interruption")).not.toBeInTheDocument();
});

it("keeps event rows concise and opens full details from any cell", async () => {
  const user = userEvent.setup();
  const value = report();
  const event = value.result!.interruptions[0];
  event.summary = "Cooling interruption during a prolonged inspection. ".repeat(12);
  event.siteId = 1n;
  event.containerId = 2n;
  event.orderId = "ORDER-01";
  event.rootCause = "A lengthy root cause recorded after investigation.";
  event.claimedExclusion = "Maintenance clause 4";
  event.exclusionStatus = "provisional";
  event.sourceRef = "Independent inspection log and signed maintenance worksheet";
  event.start = { $typeName: "google.protobuf.Timestamp", seconds: 1791000000n, nanos: 0 };
  event.observedThrough = { $typeName: "google.protobuf.Timestamp", seconds: 1791003600n, nanos: 0 };
  event.open = true;
  render(<ReportContent report={value} />);
  await user.click(screen.getByRole("button", { name: "Events" }));
  const table = screen.getByRole("table");
  expect(
    within(table)
      .getAllByRole("columnheader")
      .map((header) => header.textContent),
  ).toEqual(["Event", "Container", "Order ref", "Start", "End"]);
  expect(within(table).getByText("Ongoing")).toBeInTheDocument();
  expect(within(table).queryByText(event.rootCause)).not.toBeInTheDocument();
  expect(within(table).queryByText(event.sourceRef)).not.toBeInTheDocument();
  expect(within(table).queryByText(reportTime(event.observedThrough, "UTC"))).not.toBeInTheDocument();
  await user.click(within(table).getByText("Site 1 · Container 2"));
  const dialog = screen.getByRole("dialog", { name: "Event details" });
  expect(within(dialog).getByText(event.summary.trim())).toBeInTheDocument();
  expect(within(dialog).getByText(event.rootCause)).toBeInTheDocument();
  expect(within(dialog).getByText(event.claimedExclusion)).toBeInTheDocument();
  expect(within(dialog).getByText("Provisional")).toBeInTheDocument();
  expect(within(dialog).getByText(event.sourceRef)).toBeInTheDocument();
  expect(within(dialog).getByText("Observed through").nextElementSibling).toHaveTextContent(
    reportTime(event.observedThrough, "UTC"),
  );
  await user.click(within(dialog).getByRole("button", { name: "Close dialog" }));
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});

it("keeps omitted exclusion claims unspecified", async () => {
  const user = userEvent.setup();
  const value = report();
  value.result!.interruptions[0].claimedExclusion = "";
  value.result!.interruptions[0].exclusionStatus = "unavailable";
  render(<ReportContent report={value} />);
  await user.click(screen.getByRole("button", { name: "Events" }));
  await user.click(screen.getByText("Cooling interruption"));
  const dialog = screen.getByRole("dialog", { name: "Event details" });
  expect(within(dialog).getByText("Exclusion", { exact: true }).nextElementSibling).toHaveTextContent(/^—$/);
  expect(within(dialog).queryByText("None claimed")).not.toBeInTheDocument();
});

it.each(["{Enter}", " "])("opens event details with %s and restores focus on Escape", async (key) => {
  const user = userEvent.setup();
  const value = report();
  const event = value.result!.interruptions[0];
  event.summary = "";
  event.category = "electrical_maintenance";
  event.end = { $typeName: "google.protobuf.Timestamp", seconds: 1791003600n, nanos: 0 };
  render(<ReportContent report={value} />);
  await user.click(screen.getByRole("button", { name: "Events" }));
  const row = screen.getByText("Electrical maintenance").closest("tr")!;
  expect(within(row).getByText(reportTime(event.end, "UTC"))).toBeInTheDocument();
  row.focus();
  await user.keyboard(key);
  const dialog = screen.getByRole("dialog", { name: "Event details" });
  expect(within(dialog).queryByText("Observed through")).not.toBeInTheDocument();
  expect(within(dialog).getByText(reportTime(event.end, "UTC"))).toBeInTheDocument();
  await user.keyboard("{Escape}");
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(row).toHaveFocus();
});

it("does not retain details when the selected event disappears from an updated report", async () => {
  const user = userEvent.setup();
  const value = report();
  const { rerender } = render(<ReportContent report={value} />);
  await user.click(screen.getByRole("button", { name: "Events" }));
  await user.click(screen.getByText("Cooling interruption"));
  expect(screen.getByRole("dialog", { name: "Event details" })).toBeInTheDocument();
  value.result!.interruptions = [];
  rerender(<ReportContent report={value} />);
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  expect(screen.getByText("No events are recorded for this report.")).toBeInTheDocument();
});

it.each([
  ["167000000000000", "provisional", "167.0 TH/s"],
  ["-167000000000000", "provisional", "-167.0 TH/s"],
  ["0", "authoritative", "0 TH/s"],
  ["NaN", "provisional", "—"],
  ["", "provisional", "—"],
  ["1000000000000", "unavailable", "—"],
])("preserves hashrate formatting and unavailable values: %s / %s", (value, status, displayed) => {
  render(
    <ReportContent
      report={create(AccountReportSchema, {
        result: { metrics: { hashrate_performance_hs: { value, unit: "H/s", status } } },
      })}
    />,
  );
  expect(within(screen.getByRole("table")).getByText(displayed)).toBeInTheDocument();
});

it("keeps unsaved column choices when polling returns the same report", async () => {
  const user = userEvent.setup();
  const { rerender } = render(<ReportContent report={report()} />);
  await user.click(screen.getByRole("button", { name: "Manage result columns" }));
  const hashrate = screen.getByRole("checkbox", { name: "Toggle Hashrate column" });
  await user.click(hashrate);
  expect(hashrate).not.toBeChecked();
  rerender(<ReportContent report={report()} />);
  expect(screen.getByRole("checkbox", { name: "Toggle Hashrate column" })).not.toBeChecked();
  await user.click(screen.getByRole("button", { name: "Save" }));
  expect(within(screen.getByRole("table")).queryByText("Hashrate")).not.toBeInTheDocument();
});

it("keeps missing curtailment restoration distinct from an ongoing incident", async () => {
  const user = userEvent.setup();
  const value = report();
  const event = value.result!.interruptions[0];
  event.summary = "Hashboard 42 curtailment";
  event.category = "curtailment_control";
  event.open = true;
  event.rootCause = "Confirmed control endpoints do not prove uninterrupted physical curtailment.";
  event.sourceRef = "curtailment-confirmation:example";
  event.observedThrough = { $typeName: "google.protobuf.Timestamp", seconds: 1791003600n, nanos: 0 };
  render(<ReportContent report={value} />);
  await user.click(screen.getByRole("button", { name: "Events" }));
  expect(screen.getByText("Restoration unconfirmed")).toBeInTheDocument();
  expect(screen.queryByText("Ongoing")).not.toBeInTheDocument();
  await user.click(screen.getByText(event.summary));
  const dialog = screen.getByRole("dialog", { name: "Event details" });
  expect(within(dialog).getByText("Reporting cutoff")).toBeInTheDocument();
  expect(within(dialog).queryByText("Observed through")).not.toBeInTheDocument();
  expect(within(dialog).getByText("Evidence basis")).toBeInTheDocument();
  expect(within(dialog).getByText(event.rootCause)).toBeInTheDocument();
});
