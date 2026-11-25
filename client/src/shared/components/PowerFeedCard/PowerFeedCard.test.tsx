import { render, screen, within } from "@testing-library/react";
import { expect, it } from "vitest";
import PowerFeedCard, { type PowerFeedView } from ".";

const base: PowerFeedView = {
  feedId: "pdu-1-outlet-05",
  confidence: "configured",
  reasons: [],
  sharedWith: [],
  recoveryCycle: null,
  reading: {
    state: "enabled",
    active: true,
    faulted: false,
    overridden: false,
    voltageV: 480,
    currentA: 0,
    powerW: 18_250,
    lineVoltagesV: [480.1, 479.9, 480],
    phaseCurrentsA: [21.9, undefined, 22],
  },
};

const value = (label: string) => screen.getByText(label).previousElementSibling?.textContent;

it("shows the outlet, its controller state and readings with units", () => {
  render(<PowerFeedCard feed={base} />);
  expect(value("Outlet")).toBe("pdu-1-outlet-05");
  expect(value("Mapping")).toBe("Configured");
  expect(value("State")).toBe("Enabled");
  expect(value("Output")).toBe("Active");
  expect(value("Fault")).toBe("None");
  expect(value("Override")).toBe("None");
  expect(value("Voltage")).toBe("480.0 V");
  expect(value("Current")).toBe("0.0 A");
  expect(value("Power")).toBe("18.3 kW");
  expect(value("Line voltages")).toBe("480.1 / 479.9 / 480.0 V");
  expect(value("Phase currents")).toBe("21.9 / — / 22.0 A");
  expect(screen.queryByRole("img", { name: /needs attention/ })).toBeNull();
  expect(screen.queryByRole("list")).toBeNull();
});

it("keeps unknown state and missing readings distinct from zero", () => {
  render(
    <PowerFeedCard
      feed={{
        ...base,
        reading: { state: "unknown", overridden: true },
        recoveryCycle: undefined,
      }}
    />,
  );
  expect(value("State")).toBe("Unknown");
  expect(value("Output")).toBe("—");
  expect(value("Override")).toBe("Set");
  expect(value("Voltage")).toBe("—");
  expect(screen.queryByText("Line voltages")).toBeNull();
  expect(screen.getByText("Power recovery status could not be read.")).toBeTruthy();
});

it("flags an ambiguous mapping and explains why", () => {
  render(
    <PowerFeedCard
      feed={{
        ...base,
        confidence: "ambiguous",
        reasons: ["feed_shared", "wiring_mismatch"],
        sharedWith: [{ tank: 1, module: 6 }],
        reading: undefined,
        readingUnavailableReason: "snapshot_not_ready",
      }}
    />,
  );
  expect(screen.getByRole("img", { name: "Power feed needs attention" })).toBeTruthy();
  const notes = within(screen.getByRole("list", { name: "Power feed notes" }));
  expect(notes.getByText("This feed is also assigned to T1 M6.")).toBeTruthy();
  expect(notes.getByText("Switch-port wiring check reports mismatch.")).toBeTruthy();
  expect(notes.getByText("The controller has not collected feed readings yet.")).toBeTruthy();
  expect(value("State")).toBe("—");
});

it("says a partial controller map may hide other hardware on the feed", () => {
  render(<PowerFeedCard feed={{ ...base, confidence: "ambiguous", reasons: ["topology_incomplete"] }} />);
  expect(screen.getByRole("img", { name: "Power feed needs attention" })).toBeTruthy();
  expect(value("Mapping")).toBe("Ambiguous");
  expect(
    within(screen.getByRole("list", { name: "Power feed notes" })).getByText(
      "The controller map is incomplete, so other hardware on this feed cannot be ruled out.",
    ),
  ).toBeTruthy();
});

it("shows an active recovery cycle with its outlets and restore time", () => {
  render(
    <PowerFeedCard
      feed={{
        ...base,
        recoveryCycle: {
          phase: "holding",
          outletIds: ["pdu-1-outlet-05"],
          restoreAfter: new Date(2026, 10, 14, 12, 0, 30),
          lastError: "",
        },
      }}
    />,
  );
  expect(screen.getByText("Power recovery cycle in progress")).toBeTruthy();
  expect(
    screen.getByText(/Outlets pdu-1-outlet-05\. Phase: holding\. Restores after 11\/14\/26 at 12:00:30PM/),
  ).toBeTruthy();
});

it("renders caller actions in the card header", () => {
  render(<PowerFeedCard feed={base} actions={<button>Feed actions</button>} />);
  expect(screen.getByRole("button", { name: "Feed actions" })).toBeTruthy();
});
