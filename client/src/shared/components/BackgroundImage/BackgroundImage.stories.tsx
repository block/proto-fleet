import BackgroundImageComponent from ".";
import Miner from "@/shared/assets/images/miner.png";

export const BackgroundImage = () => {
  return (
    <BackgroundImageComponent image={Miner}>
      <div className="flex h-screen items-center justify-center" data-theme={"dark"}>
        <div className="text-display-300 text-text-primary">ProtoFleet</div>
      </div>
    </BackgroundImageComponent>
  );
};

export default {
  title: "Shared/Background Image",
};
