import { useState } from "react";
import { action } from "@storybook/addon-actions";

import { MiningStatusMiningstatus } from "apiTypes";

import PowerWidgetComponent from "./PowerWidget";

export const PowerWidget = () => {
  const [miningStatus, setMiningStatus] = useState<MiningStatusMiningstatus>({
    status: "Mining",
  });

  const handleReboot = () => {
    action("rebooting")();
    setTimeout(() => setMiningStatus({ status: "Mining" }), 2000);
  };

  const handleSleep = () => {
    action("sleeping")();
    setTimeout(() => setMiningStatus({ status: "Stopped" }), 2000);
  };

  const handleWake = () => {
    action("waking up")();
    setTimeout(() => setMiningStatus({ status: "Mining" }), 2000);
  };

  return (
    <div className="w-96 flex justify-end">
      <PowerWidgetComponent
        shouldShowPopover
        miningStatus={miningStatus}
        onReboot={handleReboot}
        onSleep={handleSleep}
        onWake={handleWake}
      />
    </div>
  );
};

export default {
  title: "Components/Page Header/Power Widget",
};
