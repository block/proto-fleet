import PictureComponent from ".";
import ProtoRigImage from "@/shared/assets/images/ProtoRig.png";

export const Picture = () => {
  return (
    <div className="w-96 px-4">
      <PictureComponent image={ProtoRigImage} alt="Proto Rig" />
    </div>
  );
};

export default {
  title: "Shared/Picture",
};
