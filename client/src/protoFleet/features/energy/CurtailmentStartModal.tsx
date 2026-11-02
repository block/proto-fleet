import { type ReactElement } from "react";
import { CurtailmentStartModalContent } from "./CurtailmentForm";
import { CurtailmentStartModalProps } from "./curtailmentModalTypes";

function CurtailmentStartModal(props: CurtailmentStartModalProps): ReactElement | null {
  if (!props.open) {
    return null;
  }

  return <CurtailmentStartModalContent {...props} />;
}

export default CurtailmentStartModal;
