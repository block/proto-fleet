import ContainerFrigateSoftwareUpdate from "./ContainerFrigateSoftwareUpdate";
import { useMinerHosting } from "@/protoOS/contexts/MinerHostingContext";
import { useSystemInfo } from "@/protoOS/store";

/**
 * Keeps a controller software update on screen while the app waits for Frigate. Frigate is stopped during an update,
 * and stays stopped after a failure until the update is retried, so the page behind the booting screen can't be
 * reached. The controller's maintenance service still answers Status, Retry and Cancel. Nothing is shown unless an
 * update is being followed, or before this page has loaded the container's identity.
 */
export default function ContainerFrigateUpdateDowntime() {
  const systemInfo = useSystemInfo();
  const { adapter, isFleetHosted } = useMinerHosting();
  const update = adapter?.frigateServerUpdate;
  const deviceId = systemInfo?.identity.deviceId;
  if (isFleetHosted || systemInfo?.identity.deviceType !== "container" || !update || !deviceId) return null;
  return (
    <div className="fixed inset-x-0 bottom-10 z-20">
      <ContainerFrigateSoftwareUpdate key={deviceId} update={update} deviceId={deviceId} duringDowntime />
    </div>
  );
}
