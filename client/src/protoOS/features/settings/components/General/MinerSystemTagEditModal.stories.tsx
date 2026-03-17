import { useState } from "react";
import { action } from "storybook/actions";

import MinerSystemTagEditModal from "./MinerSystemTagEditModal";
import Button, { sizes, variants } from "@/shared/components/Button";

const MinerSystemTagEditModalStory = ({ currentTag }: { currentTag: string }) => {
  const [open, setOpen] = useState(true);

  return (
    <>
      <div className="mt-16 flex w-full justify-center">
        <Button onClick={() => setOpen(true)} text="Open Modal" variant={variants.primary} size={sizes.base} />
      </div>
      <MinerSystemTagEditModal
        open={open}
        currentTag={currentTag}
        onDismiss={() => setOpen(false)}
        onSaved={(tag) => {
          action("onSaved")(tag);
          setOpen(false);
        }}
      />
    </>
  );
};

export const Empty = () => <MinerSystemTagEditModalStory currentTag="" />;

export const WithExistingTag = () => <MinerSystemTagEditModalStory currentTag="PM-H132435034" />;

export default {
  title: "ProtoOS/Settings/General/MinerSystemTagEditModal",
};
