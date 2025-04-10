import PictureComponent from ".";
import R2Image from "@/shared/assets/images/R2.png";

export const Picture = () => {
  return (
    <div className="w-96 px-4">
      <PictureComponent image={R2Image} alt="Rig 2" />
    </div>
  );
};

export default {
  title: "Components (Shared)/Picture",
};
