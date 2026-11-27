import { cancelFrame, frame } from "motion/react";
import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";

import Modal from "@/shared/components/Modal";

const stallEventLoop = (milliseconds: number) => {
  const end = performance.now() + milliseconds;
  while (performance.now() < end);
};

const renderModal = () =>
  render(
    <Modal open title="Title">
      <p>Body</p>
    </Modal>,
  );

// A Motion entrance animation starts at opacity zero, which toBeVisible() rejects. These tests fail if the test
// setup lets wall-clock animation frames decide when the content becomes visible.

it("shows modal content to a query that resolves immediately", async () => {
  renderModal();
  expect(await screen.findByText("Body")).toBeVisible();
});

// Motion restarts an entrance animation at progress zero when its first frame comes more than 40 ms late. A busy
// CI worker causes such a delay, and a query that polls after that frame then finds content at opacity zero.
it("shows modal content to the first query after an event loop stall", async () => {
  renderModal();
  stallEventLoop(60);
  expect(await screen.findByText("Body")).toBeVisible();
});

// Motion frames run as microtasks in tests. A callback that runs on each frame must not keep timers from running.
it("lets timers run while a Motion callback runs on each frame", async () => {
  let frames = 0;
  const countFrame = () => {
    frames++;
  };
  frame.update(countFrame, true);
  try {
    await new Promise((resolve) => setTimeout(resolve, 0));
    expect(frames).toBeGreaterThan(1);
  } finally {
    cancelFrame(countFrame);
  }
});
