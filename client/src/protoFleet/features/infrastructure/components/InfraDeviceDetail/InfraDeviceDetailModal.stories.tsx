import { useState } from "react";
import InfraDeviceDetailModal from "./InfraDeviceDetailModal";
import Button, { variants } from "@/shared/components/Button";
import { Toaster as ToasterComponent } from "@/shared/features/toaster";

export default {
  title: "Proto Fleet/Infrastructure/InfraDeviceDetailModal",
  component: InfraDeviceDetailModal,
};

export const Online = () => {
  const [open, setOpen] = useState(true);
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? <InfraDeviceDetailModal deviceId="fan-den-1-1" onDismiss={() => setOpen(false)} /> : null}
      <ToasterComponent />
    </>
  );
};

export const Degraded = () => {
  const [open, setOpen] = useState(true);
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? <InfraDeviceDetailModal deviceId="fan-den-5-2" onDismiss={() => setOpen(false)} /> : null}
      <ToasterComponent />
    </>
  );
};

export const Offline = () => {
  const [open, setOpen] = useState(true);
  return (
    <>
      <Button variant={variants.primary} text="Open Modal" onClick={() => setOpen(true)} />
      {open ? <InfraDeviceDetailModal deviceId="fan-aus-1-3" onDismiss={() => setOpen(false)} /> : null}
      <ToasterComponent />
    </>
  );
};
