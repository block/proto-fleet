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
      {open ? (
        <AddInfraDeviceModal
          siteOptions={["Austin", "Denver"]}
          buildingOptions={[
            { siteName: "Austin", buildingName: "Building 1" },
            { siteName: "Austin", buildingName: "Building 2" },
            { siteName: "Denver", buildingName: "Denver Plant" },
          ]}
          onDismiss={() => setOpen(false)}
          onSubmit={async () => setOpen(false)}
        />
      ) : null}
    </>
  );
};
