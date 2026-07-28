import { action } from "storybook/actions";
import { DISMISSED_UPDATE_TAG_KEY } from "./UpdateCallout";
import { updatesClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { ReleaseChannel } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { useFleetStore } from "@/protoFleet/store";

export const buildUpdateStatus = (overrides?: Partial<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
  ({
    currentVersion: "v1.2.0",
    channel: ReleaseChannel.STABLE,
    updateAvailable: true,
    installCommand: "curl -fsSL https://github.com/block/proto-fleet/releases/download/v1.3.0/install.sh | sh",
    latestEligible: {
      version: "v1.3.0",
      releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.3.0",
      prerelease: false,
    },
    ...overrides,
  }) as unknown as GetUpdateStatusResponse;

// The stub installed by setupUpdateCalloutStory reads this at call time, so
// story loaders can swap the payload without caring whether loaders or
// beforeEach run first.
let calloutStatus = buildUpdateStatus();

export const setUpdateCalloutStatus = (status: GetUpdateStatusResponse) => {
  calloutStatus = status;
};

// Seeded permissions persist to the iframe's localStorage (the fleet store
// persists auth on every write), so a story interrupted before its cleanup —
// tab closed, hard reload — leaves them behind and the merge-on-rehydrate
// keeps them. The Storybook iframe never carries a real session, so an
// unauthenticated store means any persisted permissions are leftovers, not a
// baseline to keep. Run this from a meta-level beforeEach, before any
// additive seeding: story-level hooks run after it and layer on top.
export const resetPermissionsToStoryBaseline = () => {
  useFleetStore.setState((state) => ({
    auth: { ...state.auth, permissions: state.auth.isAuthenticated ? state.auth.permissions : [] },
  }));
};

// The callout renders only for instance:update holders with a successful
// GetUpdateStatus response, so seed the permission and stub the client before
// the story mounts (its poll fires on mount). Clearing the dismissal tag lets
// a remount after clicking dismiss show the callout again. Returns a cleanup
// restoring the client and store so other stories are unaffected.
export const setupUpdateCalloutStory = (status?: GetUpdateStatusResponse) => {
  if (status) {
    calloutStatus = status;
  }
  localStorage.removeItem(DISMISSED_UPDATE_TAG_KEY);

  const previousPermissions = useFleetStore.getState().auth.permissions;
  useFleetStore.setState((state) => ({
    auth: { ...state.auth, permissions: [...previousPermissions, "instance:update"] },
  }));

  const originalGetUpdateStatus = updatesClient.getUpdateStatus;
  updatesClient.getUpdateStatus = async () => {
    action("getUpdateStatus API called")({});
    return calloutStatus;
  };

  return () => {
    updatesClient.getUpdateStatus = originalGetUpdateStatus;
    useFleetStore.setState((state) => ({ auth: { ...state.auth, permissions: [...previousPermissions] } }));
  };
};
