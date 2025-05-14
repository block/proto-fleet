import BackgroundImageComponent from ".";
import LandingPageBgImage from "@/shared/assets/images/landing_page_bg.png";
import { THEMES } from "@/shared/features/preferences/constants";

export const BackgroundImage = () => {
  return (
    <BackgroundImageComponent image={LandingPageBgImage}>
      <div
        className="flex h-screen items-center justify-center"
        data-theme={THEMES.dark}
      >
        <div className="text-display-300 text-text-primary">ProtoFleet</div>
      </div>
    </BackgroundImageComponent>
  );
};

export default {
  title: "Components (Shared)/Background Image",
};
