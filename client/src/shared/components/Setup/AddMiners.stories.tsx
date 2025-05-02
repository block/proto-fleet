import { action } from "@storybook/addon-actions";
import { AddMiners as AddMinersComponent } from ".";

export const AddMiners = () => {
  return (
    <div>
      <AddMinersComponent
        onScanModeDiscover={action("scan mode discovery")}
        onIpListModeDiscover={action("IP list mode discovery")}
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Add Miners",
};
