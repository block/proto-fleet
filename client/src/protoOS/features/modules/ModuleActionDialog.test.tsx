import { fireEvent, render, screen, within } from "@testing-library/react";
import { expect, test, vi } from "vitest";
import ModuleActionDialog from "./ModuleActionDialog";
import type { useModuleActions } from "./useModuleActions";

const controls = (outcome?: ReturnType<typeof useModuleActions>["outcome"]): ReturnType<typeof useModuleActions> => ({
  actionsFor: vi.fn(),
  request: {
    action: "stop",
    generation: 1,
    target: {
      moduleIds: ["module-1"],
      names: ["T1-P1"],
      identifiers: ["T1-M1-HB1", "T1-M1-HB2", "T1-M1-HB3"],
    },
  },
  pending: false,
  outcome,
  confirm: vi.fn(),
  dismiss: vi.fn(),
});

test("shows command errors in the open dialog", () => {
  render(<ModuleActionDialog controls={controls({ error: "The selected modules changed." })} />);

  const dialog = screen.getByRole("dialog", { name: "Stop mining" });
  expect(within(dialog).getByRole("alert")).toHaveTextContent("The selected modules changed.");
  expect(within(dialog).getByText("3 hashboards in 1 module:")).toBeInTheDocument();
  expect(within(dialog).getByText("T1-P1")).toBeInTheDocument();
});

test("reports accepted counts and board failures for a partial result", () => {
  render(
    <ModuleActionDialog
      controls={controls({
        result: {
          accepted: 2,
          failures: [{ identifier: "T1-M1-HB3", message: "Board is disconnected." }],
        },
      })}
    />,
  );

  const status = screen.getByRole("status");
  expect(status).toHaveTextContent("Command accepted for 2 of 3 hashboards.");
  expect(status).toHaveTextContent("Some hashboards did not accept the command.");
  expect(status).toHaveTextContent("T1-M1-HB3: Board is disconnected.");
});

test("reports a refresh warning after the command is fully accepted", () => {
  render(
    <ModuleActionDialog
      controls={controls({
        result: { accepted: 3, failures: [] },
        refreshFailed: true,
      })}
    />,
  );

  const status = screen.getByRole("status");
  expect(status).toHaveTextContent("Command accepted for 3 of 3 hashboards.");
  expect(status).toHaveTextContent("Could not refresh module status. Waiting for the next update.");
  expect(status).not.toHaveTextContent("Some hashboards did not accept the command.");
});

test("Done dismisses a completed command dialog", () => {
  const dialogControls = controls({ result: { accepted: 3, failures: [] } });
  render(<ModuleActionDialog controls={dialogControls} />);

  const dialog = screen.getByRole("dialog", { name: "Stop mining" });
  fireEvent.click(within(dialog).getAllByRole("button", { name: "Done" })[0]);

  expect(dialogControls.dismiss).toHaveBeenCalled();
});
