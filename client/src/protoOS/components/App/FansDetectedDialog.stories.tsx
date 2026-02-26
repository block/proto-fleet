import { action } from "storybook/actions";

import FansDetectedDialog from "./FansDetectedDialog";

export const Default = () => {
  return <FansDetectedDialog onContinue={action("onContinue")} onSwitchToAirCooled={action("onSwitchToAirCooled")} />;
};

export const Loading = () => {
  return (
    <FansDetectedDialog
      onContinue={action("onContinue")}
      onSwitchToAirCooled={action("onSwitchToAirCooled")}
      isLoading={true}
    />
  );
};

export default {
  title: "protoOS/Dialogs/Fans Detected Dialog",
};
