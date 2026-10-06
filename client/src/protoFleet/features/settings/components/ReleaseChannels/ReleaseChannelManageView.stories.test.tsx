import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { composeStories } from "@storybook/react-vite";

import * as stories from "./ReleaseChannelManageView.stories";

const { Settled, ScopeOverlap } = composeStories(stories);

afterEach(cleanup);

it("keeps the settled model summary and miner drilldowns consistent", async () => {
  render(<Settled />);
  fireEvent.click(screen.getByTestId("view-miners-Rig"));
  expect(await screen.findAllByText("On assigned version")).toHaveLength(6);
  expect(screen.queryByText("Not on assigned version")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Done" }));

  fireEvent.click(screen.getByTestId("view-miners-S21"));
  expect(await screen.findByTestId("channel-miner-s21-001")).toHaveTextContent("3.0.0");
  expect(screen.getByTestId("channel-miner-s21-002")).toHaveTextContent("3.0.0");
});

it("shows scope overlap with a different channel", async () => {
  render(<ScopeOverlap />);
  expect(screen.getByRole("heading", { name: "Canary" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Channel settings" }));
  const settings = within(screen.getByTestId("channel-settings-modal"));
  expect(settings.getByLabelText("Name")).toHaveValue("Canary");
  expect(await settings.findByTestId("scope-conflicts")).toHaveTextContent("Overlaps Production (2 miners)");
});
