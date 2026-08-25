import { MemoryRouter } from "react-router-dom";
import { act, cleanup, render } from "@testing-library/react";
import { afterEach, describe, it } from "vitest";
import { composeStories } from "@storybook/react";

import * as stories from "./BetweenChannelRollout.stories";

const { OneModelPartialRollout, TwoModelsInDifferentActiveStates, ChildLocalErrorAndLoading, PhoneLayout } =
  composeStories(stories);

async function runStory(Story: typeof OneModelPartialRollout, width?: number) {
  if (width !== undefined) {
    document.body.style.setProperty("--phone-max-width", "639");
    Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  }
  const { container } = render(
    <MemoryRouter>
      <Story />
    </MemoryRouter>,
  );
  if (width !== undefined) {
    act(() => window.dispatchEvent(new Event("resize")));
  }
  await Story.play?.({ canvasElement: container });
}

describe("BetweenChannelRollout Storybook plays", () => {
  afterEach(() => {
    cleanup();
    act(() => {
      document.body.style.removeProperty("--phone-max-width");
      Object.defineProperty(window, "innerWidth", { configurable: true, value: 1024 });
      window.dispatchEvent(new Event("resize"));
    });
  });

  it("runs child expansion assertions", async () => {
    await runStory(OneModelPartialRollout);
  });

  it("runs independent child controls and live-region assertions", async () => {
    await runStory(TwoModelsInDifferentActiveStates);
  });

  it("runs child-local retry and sibling-preservation assertions", async () => {
    await runStory(ChildLocalErrorAndLoading);
  });

  it("runs the phone-width primary and overflow action assertions", async () => {
    await runStory(PhoneLayout, 390);
  });
});
