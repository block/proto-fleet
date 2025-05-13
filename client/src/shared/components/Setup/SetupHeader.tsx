import { Logo } from "@/shared/assets/icons";
import Divider from "@/shared/components/Divider";

const SetupHeader = () => {
  return (
    <div className="mb-8">
      <div className="flex items-center p-6">
        <Logo width="w-22" />
      </div>
      <Divider />
    </div>
  );
};

export default SetupHeader;
