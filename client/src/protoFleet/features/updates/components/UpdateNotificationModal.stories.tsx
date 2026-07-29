import type { StoryFn } from "@storybook/react-vite";

import UpdateNotificationModal from "./UpdateNotificationModal";
import type { ReleaseInfo } from "@/protoFleet/api/generated/updates/v1/updates_pb";

const release: ReleaseInfo = {
  version: "v1.3.0",
  releaseNotesUrl: "https://github.com/block/proto-fleet/releases/tag/v1.3.0",
  prerelease: false,
} as ReleaseInfo;

export default {
  title: "Proto Fleet/Updates/UpdateNotificationModal",
  component: UpdateNotificationModal,
};

export const Default: StoryFn = () => (
  <UpdateNotificationModal
    open
    release={release}
    installCommand={`bash <(curl -fsSL "https://github.com/block/proto-fleet/releases/download/v1.3.0/install.sh") v1.3.0`}
    onDismiss={() => {}}
  />
);
