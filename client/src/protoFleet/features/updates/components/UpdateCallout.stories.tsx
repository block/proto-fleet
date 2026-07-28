import type { StoryFn } from "@storybook/react-vite";
import { action } from "storybook/actions";
import UpdateCallout, { DISMISSED_UPDATE_TAG_KEY } from "./UpdateCallout";
import { updatesClient } from "@/protoFleet/api/clients";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { ReleaseChannel } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { useFleetStore } from "@/protoFleet/store";

const buildStatus = (overrides?: Partial<GetUpdateStatusResponse>): GetUpdateStatusResponse =>
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

// Each story assigns its payload via a loader (loaders run outside render);
// the stub installed in beforeEach reads it at call time.
let storyStatus = buildStatus();

export default {
  title: "Proto Fleet/Updates/UpdateCallout",
  component: UpdateCallout,
  // The callout renders only for instance:update holders with a successful
  // GetUpdateStatus response, so seed the permission and stub the client
  // before the story mounts (its poll fires on mount). Clearing the dismissal
  // tag lets a remount after clicking dismiss show the callout again.
  beforeEach: () => {
    localStorage.removeItem(DISMISSED_UPDATE_TAG_KEY);

    const previousPermissions = useFleetStore.getState().auth.permissions;
    useFleetStore.setState((state) => ({
      auth: { ...state.auth, permissions: [...previousPermissions, "instance:update"] },
    }));

    const originalGetUpdateStatus = updatesClient.getUpdateStatus;
    updatesClient.getUpdateStatus = async () => {
      action("getUpdateStatus API called")({});
      return storyStatus;
    };

    return () => {
      updatesClient.getUpdateStatus = originalGetUpdateStatus;
      useFleetStore.setState((state) => ({ auth: { ...state.auth, permissions: previousPermissions } }));
    };
  },
};

// group/nav mirrors the nav panel wrapper: at laptop widths the collapsed
// icon-dot shows and hovering the group swaps in the full card, exactly as in
// the real nav footer.
const CalloutInNavFooter = () => (
  <div className="group/nav w-64">
    <UpdateCallout />
  </div>
);

const withStatus = (status: GetUpdateStatusResponse) => [
  () => {
    storyStatus = status;
  },
];

export const UpdateAvailable: StoryFn = () => <CalloutInNavFooter />;
UpdateAvailable.loaders = withStatus(buildStatus());

export const WithoutReleaseNotesLink: StoryFn = () => <CalloutInNavFooter />;
WithoutReleaseNotesLink.loaders = withStatus(
  buildStatus({
    latestEligible: {
      version: "v1.3.0",
      releaseNotesUrl: "",
      prerelease: false,
    } as GetUpdateStatusResponse["latestEligible"],
  }),
);

export const ReleaseCandidateOffer: StoryFn = () => <CalloutInNavFooter />;
ReleaseCandidateOffer.loaders = withStatus(
  buildStatus({
    channel: ReleaseChannel.STABLE_AND_RC,
    installCommand: "curl -fsSL https://github.com/block/proto-fleet/releases/download/v1.4.0-rc.1/install.sh | sh",
    latestEligible: {
      version: "v1.4.0-rc.1",
      releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
      prerelease: true,
    } as GetUpdateStatusResponse["latestEligible"],
  }),
);
