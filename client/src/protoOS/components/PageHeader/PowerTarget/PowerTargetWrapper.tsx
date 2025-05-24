import PowerTarget from "./PowerTarget";
import { PopoverProvider } from "@/shared/components/Popover";

const PowerTargetWrapper = () => {
  return (
    <PopoverProvider>
      <PowerTarget />
    </PopoverProvider>
  );
};

export default PowerTargetWrapper;
