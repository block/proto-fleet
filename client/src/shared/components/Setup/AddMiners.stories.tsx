import { action } from "@storybook/addon-actions";
import { AddMiners as AddMinersComponent } from ".";

export const AddMiners = () => {
  return (
    <div>
      <AddMinersComponent
        loading={false}
        onScanModeDiscover={action("scan mode discovery")}
        onMdnsModeDiscover={action("mdns mode discovery")}
        onIpListModeDiscover={action("IP list mode discovery")}
        scanResults={
          <div>
            <p>Scan results</p>
          </div>
        }
      />
    </div>
  );
};

export default {
  title: "Components (Shared)/Setup/Add Miners",
};
