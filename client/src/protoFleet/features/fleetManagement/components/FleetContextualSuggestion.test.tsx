import { render, screen } from "@testing-library/react";
import { describe, expect, test, vi } from "vitest";
import userEvent from "@testing-library/user-event";

import FleetContextualSuggestion from "./FleetContextualSuggestion";

describe("FleetContextualSuggestion", () => {
  test("renders contextual copy and dispatches action", async () => {
    const user = userEvent.setup();
    const onReview = vi.fn();
    const onDismiss = vi.fn();

    render(
      <FleetContextualSuggestion
        title="24 unassigned miners from 10.90.12.40-10.90.12.63 look like a rack."
        detail="Seen in nearby IPs. Mostly Proto Rig."
        action={{ label: "Review", onClick: onReview }}
        onDismiss={onDismiss}
      />,
    );

    expect(screen.getByText("24 unassigned miners from 10.90.12.40-10.90.12.63 look like a rack.")).toBeVisible();
    expect(screen.getByText("Seen in nearby IPs. Mostly Proto Rig.")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Review" }));
    await user.click(screen.getByRole("button", { name: "Dismiss suggestion" }));

    expect(onReview).toHaveBeenCalledTimes(1);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });
});
