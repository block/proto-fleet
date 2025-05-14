import { Logo } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";

const SetupHeader = () => {
  return (
    <div>
      <div className="flex h-16 items-center pl-6">
        <Logo width="w-22" />
      </div>
      <Divider />
    </div>
  );
};

export default SetupHeader;
