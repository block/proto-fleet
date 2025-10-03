import { action } from "storybook/actions";

import FansDetectedDialog from "./FansDetectedDialog";

export const Default = () => {
  return (
    <FansDetectedDialog
      onConfirmImmersion={action("confirmed immersion cooling")}
      onSwitchToAirCooled={action("switched to air cooled")}
      show={true}
    />
  );
};

export const Loading = () => {
  return (
    <FansDetectedDialog
      onConfirmImmersion={action("confirmed immersion cooling")}
      onSwitchToAirCooled={action("switched to air cooled")}
      isLoading={true}
      show={true}
    />
  );
};

export default {
  title: "protoOS/Dialogs/Fans Detected Dialog",
};
