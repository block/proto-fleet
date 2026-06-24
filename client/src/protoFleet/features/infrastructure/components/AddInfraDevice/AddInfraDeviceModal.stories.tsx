import { useState } from "react";

import AddInfraDeviceModal from "./AddInfraDeviceModal";
import Button, { variants } from "@/shared/components/Button";

export default {
  title: "Proto Fleet/Infrastructure/AddInfraDeviceModal",
  component: AddInfraDeviceModal,
};

export const Default = () => {
  const [open, setOpen] = useState(true);
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? <AddInfraDeviceModal onDismiss={() => setOpen(false)} onSuccess={() => setOpen(false)} /> : null}
    </>
  );
};
