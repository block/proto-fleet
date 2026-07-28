import type { StoryFn } from "@storybook/react-vite";
import UpdateCallout from "./UpdateCallout";
import {
  buildUpdateStatus,
  resetPermissionsToStoryBaseline,
  setUpdateCalloutStatus,
  setupUpdateCalloutStory,
} from "./updateCalloutStorySetup";
import type { GetUpdateStatusResponse } from "@/protoFleet/api/generated/updates/v1/updates_pb";
import { ReleaseChannel } from "@/protoFleet/api/generated/updates/v1/updates_pb";

export default {
  title: "Proto Fleet/Updates/UpdateCallout",
  component: UpdateCallout,
  beforeEach: () => {
    resetPermissionsToStoryBaseline();
    return setupUpdateCalloutStory();
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

// Loaders run outside render, so per-story payloads swap without side effects
// in components.
const withStatus = (status: GetUpdateStatusResponse) => [
  () => {
    setUpdateCalloutStatus(status);
  },
];

export const UpdateAvailable: StoryFn = () => <CalloutInNavFooter />;
UpdateAvailable.loaders = withStatus(buildUpdateStatus());

export const WithoutReleaseNotesLink: StoryFn = () => <CalloutInNavFooter />;
WithoutReleaseNotesLink.loaders = withStatus(
  buildUpdateStatus({
    latestEligible: {
      version: "v1.3.0",
      releaseNotesUrl: "",
      prerelease: false,
    } as GetUpdateStatusResponse["latestEligible"],
  }),
);

export const ReleaseCandidateOffer: StoryFn = () => <CalloutInNavFooter />;
ReleaseCandidateOffer.loaders = withStatus(
  buildUpdateStatus({
    channel: ReleaseChannel.STABLE_AND_RC,
    installCommand: "curl -fsSL https://github.com/block/proto-fleet/releases/download/v1.4.0-rc.1/install.sh | sh",
    latestEligible: {
      version: "v1.4.0-rc.1",
      releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.4.0-rc.1",
      prerelease: true,
    } as GetUpdateStatusResponse["latestEligible"],
  }),
);
