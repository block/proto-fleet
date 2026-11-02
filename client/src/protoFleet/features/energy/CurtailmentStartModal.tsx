import type { ReactElement } from "react";
import { CreateCurtailmentModal, EditCurtailmentModal, ResponseProfileModal } from "./CurtailmentModalShells";
import type { CurtailmentStartModalProps } from "./curtailmentModalTypes";
function CurtailmentStartModal(props: CurtailmentStartModalProps): ReactElement | null {
  if (!props.open) return null;
  if (props.variant === "responseProfile") return <ResponseProfileModal {...props} />;
  if (props.mode === "edit") return <EditCurtailmentModal {...props} />;
  return <CreateCurtailmentModal {...props} />;
}
export default CurtailmentStartModal;
