import { describe, expect, it, vi } from "vitest";

import { buildModuleActions, type ModuleActionType } from "./moduleActions";

describe("buildModuleActions", () => {
  it("returns View, Blink LEDs, Reboot, Sleep in order", () => {
    const actions = buildModuleActions(() => {});
    expect(actions.map((a) => a.type)).toEqual<ModuleActionType[]>(["view", "blink", "reboot", "sleep"]);
    expect(actions.map((a) => a.label)).toEqual(["View", "Blink LEDs", "Reboot", "Sleep"]);
  });

  it("does not include a net-new isolate action", () => {
    const actions = buildModuleActions(() => {});
    // Sleep (deviceActions.shutdown) replaces isolate — no isolate string is introduced.
    expect(actions.map((a) => a.type)).not.toContain("isolate");
    expect(actions.some((a) => a.label.toLowerCase().includes("isolate"))).toBe(false);
  });

  it("groups the View navigation action away from the device actions with a single divider", () => {
    const actions = buildModuleActions(() => {});
    const withDivider = actions.filter((a) => a.showGroupDivider);
    expect(withDivider.map((a) => a.type)).toEqual(["view"]);
  });

  it("gives every action a stable testId", () => {
    const actions = buildModuleActions(() => {});
    expect(actions.map((a) => a.testId)).toEqual([
      "module-action-view",
      "module-action-blink",
      "module-action-reboot",
      "module-action-sleep",
    ]);
  });

  it("invokes the callback with the action's own type on click", () => {
    const onAction = vi.fn();
    const actions = buildModuleActions(onAction);
    for (const action of actions) action.onClick();
    expect(onAction.mock.calls.map(([type]) => type)).toEqual(["view", "blink", "reboot", "sleep"]);
  });
});
