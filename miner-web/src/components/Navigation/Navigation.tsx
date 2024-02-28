import PauseIcon from "icons/Pause";
import PowerIcon from "icons/Power";

import ControllerIpAddressInfo from "./InfoItems/ControllerIpAddressInfo";
import ControllerMacAddressInfo from "./InfoItems/ControllerMacAddressInfo";
import HashboardInfo from "./InfoItems/HashboardInfo";
import PoolInfo, { PoolProps } from "./InfoItems/PoolInfo";
import NavigationButton from "./NavigationButton";
import NavigationItems from "./NavigationItems";

import "./styles.css";

interface ApiProps {
  loading?: boolean;
}

interface InfoItemProps extends ApiProps {
  value?: string;
}

interface HashboardProps extends ApiProps {
  value?: string[];
}

interface NavigationProps {
  controllerIp?: InfoItemProps;
  controllerMac?: InfoItemProps;
  hashboardSerials?: HashboardProps;
  poolInfo?: PoolProps;
  onClickReboot: () => void;
  onClickSleep: () => void;
}

const Navigation = ({
  controllerIp,
  controllerMac,
  hashboardSerials,
  poolInfo,
  onClickReboot,
  onClickSleep,
}: NavigationProps) => {
  return (
    <div className="sidebar-wrapper w-[280px] h-auto min-h-screen p-6 flex flex-col border-r border-border-primary/30">
      <div className="grow">
        <div className="text-heading-300 mb-6 text-text-primary/70">
          Proto<span className="text-text-primary">Mine</span>
        </div>
        <NavigationItems />
      </div>

      <div className="border-t border-border-primary/10 mt-11 mb-3" />

      <PoolInfo
        status={poolInfo?.status}
        url={poolInfo?.url}
        loading={poolInfo?.loading}
        error={poolInfo?.error}
      />
      <HashboardInfo
        hashboardSerials={hashboardSerials?.value}
        loading={hashboardSerials?.loading}
      />
      <ControllerIpAddressInfo
        ipAddress={controllerIp?.value}
        loading={controllerIp?.loading}
      />
      <ControllerMacAddressInfo
        macAddress={controllerMac?.value}
        loading={controllerMac?.loading}
      />

      <div className="border-t border-border-primary/10 mb-3" />

      <div className="flex space-x-3">
        <NavigationButton
          text="Sleep"
          className="w-full"
          prefixIcon={<PauseIcon />}
          onClick={onClickSleep}
        />
        <NavigationButton
          text="Reboot"
          className="w-full"
          prefixIcon={<PowerIcon />}
          onClick={onClickReboot}
        />
      </div>
    </div>
  );
};

export default Navigation;
