import { useLocation, useNavigate } from "react-router-dom";
import HashboardTemperatureWrapper from "@/protoOS/features/diagnostic/components/HashboardTemperature/HashboardTemperatureWrapper";
import Modal from "@/shared/components/Modal";

const ModuleHashboardOverlay = () => {
  const navigate = useNavigate();
  const { search } = useLocation();
  return (
    <Modal
      size="fullscreen"
      showHeader={false}
      dialogLabel="Hashboards"
      manageFocus
      onDismiss={() => navigate({ pathname: "..", search }, { relative: "path" })}
      bodyClassName="p-0"
    >
      <HashboardTemperatureWrapper />
    </Modal>
  );
};
export default ModuleHashboardOverlay;
