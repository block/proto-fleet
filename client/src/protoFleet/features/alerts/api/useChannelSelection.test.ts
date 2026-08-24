import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import * as api from "@/protoFleet/features/alerts/api/alertsApi";
import { useChannelSelection } from "@/protoFleet/features/alerts/api/useChannelSelection";
import type { Channel } from "@/protoFleet/features/alerts/types";
import { pushToast } from "@/shared/features/toaster";

vi.mock("@/shared/features/toaster");
vi.mock("@/protoFleet/features/alerts/api/alertsApi");

const channel: Channel = {
  id: "5",
  organization_id: "7",
  name: "ops",
  kind: "slack",
  webhook: null,
  slack: {},
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  validated_at: null,
  validation_state: "pending",
};

describe("useChannelSelection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("fetches channels once per session, only while active", async () => {
    vi.mocked(api.listChannels).mockResolvedValue([channel]);
    const { result, rerender } = renderHook(({ active }) => useChannelSelection(active), {
      initialProps: { active: false },
    });
    expect(api.listChannels).not.toHaveBeenCalled();

    rerender({ active: true });
    await waitFor(() => expect(result.current.channelsLoaded).toBe(true));
    expect(result.current.channels).toEqual([channel]);

    rerender({ active: false });
    rerender({ active: true });
    expect(api.listChannels).toHaveBeenCalledTimes(1);
  });

  it("re-arms after a failed fetch, so re-entering the selected mode retries", async () => {
    vi.mocked(api.listChannels).mockRejectedValueOnce(new Error("boom")).mockResolvedValueOnce([channel]);
    const { result, rerender } = renderHook(({ active }) => useChannelSelection(active), {
      initialProps: { active: true },
    });
    await waitFor(() => expect(pushToast).toHaveBeenCalledTimes(1));
    expect(result.current.channelsLoaded).toBe(false);

    // The user toggles back to "All channels" and then to "Selected channels" again.
    rerender({ active: false });
    rerender({ active: true });
    await waitFor(() => expect(result.current.channelsLoaded).toBe(true));
    expect(result.current.channels).toEqual([channel]);
    expect(api.listChannels).toHaveBeenCalledTimes(2);
  });
});
