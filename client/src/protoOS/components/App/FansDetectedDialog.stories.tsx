import { action } from "storybook/actions";

import FansDetectedDialog from "./FansDetectedDialog";

export const Default = () => {
  return (
    <FansDetectedDialog
      onRetry={action("confirmed immersion cooling")}
      onCancel={action("switched to air cooled")}
      show={true}
    />
  );
};

export const Loading = () => {
  return (
    <FansDetectedDialog
      onRetry={action("confirmed immersion cooling")}
      onCancel={action("switched to air cooled")}
      isLoading={true}
      show={true}
    />
  );
};

export default {
  title: "protoOS/Dialogs/Fans Detected Dialog",
};
