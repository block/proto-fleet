import { action } from "storybook/actions";

import { GlobalActionsWidget as GlobalActionsWidgetComponent } from "./GlobalActionsWidget";
import { PopoverProvider } from "@/shared/components/Popover";

export const GlobalActionsWidgetStory = () => {
  return (
    <div className="flex w-96 justify-end">
      <PopoverProvider>
        <GlobalActionsWidgetComponent onDownloadLogs={action("download-logs")} onBlinkLEDs={action("blink-leds")} />
      </PopoverProvider>
    </div>
  );
};

export default {
  title: "Proto OS/Page Header/Global Actions Widget",
};
