import { ReactNode, useEffect, useMemo, useState } from "react";
import Divider from "@/shared/components/Divider";

interface ChildrenProps {
  children: ReactNode;
}

const TokenWrapper = ({ children }: ChildrenProps) => {
  return <div className="flex flex-col space-y-4">{children}</div>;
};

interface HeaderProps {
  title: string;
}

const Header = ({ title }: HeaderProps) => {
  return <div className="text-heading-300 text-text-primary">{title}</div>;
};

const Row = ({ children }: ChildrenProps) => {
  return <div className="flex space-x-4">{children}</div>;
};

interface SwatchProps {
  className: string;
  hex?: string;
}

const Swatch = ({ className, hex }: SwatchProps) => {
  return (
    <div className={`${className} flex h-40 w-44 flex-col rounded-xl p-2 shadow-200`}>
      <div className="grow">{hex}</div>
      {className.split(" ")[0].split("bg-")[1]}
    </div>
  );
};

const SectionDivider = () => {
  return <Divider className="mt-8 mb-4" />;
};

const useDarkMode = () => {
  const [isDark, setIsDark] = useState(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
    return mediaQuery.matches;
  });

  useEffect(() => {
    const mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");

    const handleChange = (event: MediaQueryListEvent) => {
      setIsDark(event.matches);
    };

    // Add listener for changes
    mediaQuery.addEventListener("change", handleChange);

    // Cleanup listener on unmount
    return () => {
      mediaQuery.removeEventListener("change", handleChange);
    };
  }, []);

  return isDark;
};

export const Colors = () => {
  const isDark = useDarkMode();

  // typography
  const textPrimaryHex = useMemo(() => (isDark ? "#FFFFFF" : "#000000"), [isDark]);
  const textContrastHex = useMemo(() => (isDark ? "#000000" : "#FFFFFF"), [isDark]);

  // surface
  const surfaceDefaultHex = useMemo(() => (isDark ? "#000000" : "#FFFFFF"), [isDark]);
  const surfaceBaseHex = useMemo(() => (isDark ? "#000000" : "#FFFFFF"), [isDark]);
  const surfaceElevatedBaseHex = useMemo(() => (isDark ? "#101010" : "#FFFFFF"), [isDark]);
  const surface20Hex = useMemo(() => (isDark ? "#202020" : "#C0C0C0"), [isDark]);
  const surface10Hex = useMemo(() => (isDark ? "#303030" : "#E0E0E0"), [isDark]);
  const surface5Hex = useMemo(() => (isDark ? "#505050" : "#F2F2F2"), [isDark]);

  // border
  const borderPrimaryHex = useMemo(() => (isDark ? "#FFFFFF" : "#000000"), [isDark]);

  // core
  const corePrimaryHex = useMemo(() => (isDark ? "#FFFFFF" : "#000000"), [isDark]);
  const corePrimaryFillHex = useMemo(() => (isDark ? "#FFFFFF" : "#000000"), [isDark]);
  const coreAccentTextHex = useMemo(() => (isDark ? "#F9732A" : "#331200"), [isDark]);

  return (
    <>
      <TokenWrapper>
        <Header title="Typography" />
        <Row>
          <Swatch className="bg-text-primary text-text-contrast" hex={`${textPrimaryHex} 90%`} />
          <Swatch className="bg-text-primary-70 text-text-contrast" hex={`${textPrimaryHex} 70%`} />
          <Swatch className="bg-text-primary-50 text-text-contrast" hex={`${textPrimaryHex} 50%`} />
          <Swatch className="bg-text-primary-30 text-text-contrast" hex={`${textPrimaryHex} 30%`} />
          <Swatch
            className="bg-text-contrast-70 text-text-primary"
            hex={`${textContrastHex} ${isDark ? "80%" : "70%"}`}
          />
          <Swatch className="bg-text-contrast text-text-primary" hex={textContrastHex} />
        </Row>
        <Row>
          <Swatch className="bg-text-emphasis text-text-contrast" hex="#FE7C00" />
          <Swatch className="bg-text-accent text-text-contrast" hex="#FE7C00 80%" />
          <Swatch className="bg-text-success text-text-contrast" hex="#9DD31A" />
          <Swatch className="bg-text-warning text-text-contrast" hex="#FD8A00" />
          <Swatch className="bg-text-critical text-text-contrast" hex="#FA2B37" />
        </Row>
      </TokenWrapper>
      <SectionDivider />
      <TokenWrapper>
        <Header title="Surface" />
        <Row>
          <Swatch className="bg-surface-default text-text-primary" hex={`${surfaceDefaultHex} 2%`} />
          <Swatch className="bg-surface-base text-text-primary" hex={surfaceBaseHex} />
          <Swatch className="bg-surface-elevated-base text-text-primary" hex={surfaceElevatedBaseHex} />
        </Row>
        <Row>
          <Swatch className="bg-surface-20 text-text-primary" hex={surface20Hex} />
          <Swatch className="bg-surface-10 text-text-primary" hex={surface10Hex} />
          <Swatch className="bg-surface-5 text-text-primary" hex={surface5Hex} />
          <Swatch className="bg-surface-overlay text-text-primary" hex="#000000 5%" />
        </Row>
      </TokenWrapper>
      <SectionDivider />
      <TokenWrapper>
        <Header title="Border" />
        <Row>
          <Swatch className="bg-border-primary text-text-contrast" hex={`${borderPrimaryHex} 90%`} />
          <Swatch className="bg-border-20 text-text-primary" hex={`${borderPrimaryHex} ${isDark ? "30%" : "20%"}`} />
          <Swatch className="bg-border-10 text-text-primary" hex={`${borderPrimaryHex} ${isDark ? "20%" : "10%"}`} />
          <Swatch className="bg-border-5 text-text-primary" hex={`${borderPrimaryHex} ${isDark ? "10%" : "5%"}`} />
        </Row>
      </TokenWrapper>
      <SectionDivider />
      <TokenWrapper>
        <Header title="Core" />
        <Row>
          <Swatch className="bg-core-primary-fill text-text-contrast" hex={`${corePrimaryFillHex} 90%`} />
          <Swatch className="bg-core-primary-80 text-text-contrast" hex={`${corePrimaryHex} 80%`} />
          <Swatch className="bg-core-primary-50 text-text-contrast" hex="#000000 50%" />
          <Swatch className="bg-core-primary-20 text-text-primary" hex="#000000 20%" />
          <Swatch className="bg-core-primary-10 text-text-primary" hex="#000000 10%" />
          <Swatch className="bg-core-primary-5 text-text-primary" hex="#000000 5%" />
        </Row>
        <Row>
          <Swatch className="bg-core-accent-fill text-text-contrast" hex="#FE7C00" />
          <Swatch className="bg-core-accent-text text-text-contrast" hex={coreAccentTextHex} />
          <Swatch className="bg-core-accent-80 text-text-contrast" hex="#FE7C00 80%" />
          <Swatch className="bg-core-accent-50 text-text-primary" hex="#FE7C00 50%" />
          <Swatch className="bg-core-accent-20 text-text-primary" hex="#FE7C00 20%" />
          <Swatch className="bg-core-accent-10 text-text-primary" hex="#FE7C00 10%" />
        </Row>
        <Row>
          <Swatch className="bg-core-indigo-fill text-text-contrast" hex="#783EED" />
          <Swatch className="bg-core-indigo-text text-text-contrast" hex="#371C6D" />
          <Swatch className="bg-core-indigo-80 text-text-contrast" hex="#783EED 80%" />
          <Swatch className="bg-core-indigo-50 text-text-primary" hex="#783EED 50%" />
          <Swatch className="bg-core-indigo-20 text-text-primary" hex="#783EED 20%" />
          <Swatch className="bg-core-indigo-10 text-text-primary" hex="#783EED 10%" />
        </Row>
      </TokenWrapper>
      <SectionDivider />
      <TokenWrapper>
        <Header title="Intent" />
        <Row>
          <Swatch className="bg-intent-info-fill text-text-contrast" hex="#2690C7" />
          <Swatch className="bg-intent-info-text text-text-contrast" hex={isDark ? "#2690C7" : "#015377"} />
          <Swatch className="bg-intent-info-80 text-text-primary" hex={`#2690C7 ${isDark ? "90%" : "80%"}`} />
          <Swatch className="bg-intent-info-50 text-text-primary" hex={`#2690C7 ${isDark ? "60%" : "50%"}`} />
          <Swatch className="bg-intent-info-20 text-text-primary" hex={`#2690C7 ${isDark ? "30%" : "20%"}`} />
          <Swatch className="bg-intent-info-10 text-text-primary" hex={`#2690C7 ${isDark ? "20%" : "10%"}`} />
        </Row>
        <Row>
          <Swatch className="bg-intent-success-fill text-text-contrast" hex="#38A600" />
          <Swatch className="bg-intent-success-text text-text-contrast" hex={isDark ? "#38A600" : "#063F25"} />
          <Swatch className="bg-intent-success-80 text-text-primary" hex={`#38A600 ${isDark ? "90%" : "80%"}`} />
          <Swatch className="bg-intent-success-50 text-text-primary" hex={`#38A600 ${isDark ? "60%" : "50%"}`} />
          <Swatch className="bg-intent-success-20 text-text-primary" hex={`#38A600 ${isDark ? "30%" : "20%"}`} />
          <Swatch className="bg-intent-success-10 text-text-primary" hex={`#38A600 ${isDark ? "20%" : "10%"}`} />
        </Row>
        <Row>
          <Swatch className="bg-intent-warning-fill text-text-contrast" hex="#FD8A00" />
          <Swatch className="bg-intent-warning-text text-text-contrast" hex={isDark ? "#FD8A00" : "#874900"} />
          <Swatch className="bg-intent-warning-80 text-text-primary" hex={`#FD8A00 ${isDark ? "90%" : "80%"}`} />
          <Swatch className="bg-intent-warning-50 text-text-primary" hex={`#FD8A00 ${isDark ? "60%" : "50%"}`} />
          <Swatch className="bg-intent-warning-20 text-text-primary" hex={`#FD8A00 ${isDark ? "30%" : "20%"}`} />
          <Swatch className="bg-intent-warning-10 text-text-primary" hex={`#FD8A00 ${isDark ? "20%" : "10%"}`} />
        </Row>
        <Row>
          <Swatch className="bg-intent-critical-fill text-text-contrast" hex="#FA2B37" />
          <Swatch className="bg-intent-critical-text text-text-contrast" hex={isDark ? "#FA2B37" : "#74140D"} />
          <Swatch className="bg-intent-critical-80 text-text-contrast" hex={`#FA2B37 ${isDark ? "90%" : "80%"}`} />
          <Swatch className="bg-intent-critical-50 text-text-primary" hex={`#FA2B37 ${isDark ? "60%" : "50%"}`} />
          <Swatch className="bg-intent-critical-20 text-text-primary" hex={`#FA2B37 ${isDark ? "30%" : "20%"}`} />
          <Swatch className="bg-intent-critical-10 text-text-primary" hex={`#FA2B37 ${isDark ? "20%" : "10%"}`} />
        </Row>
      </TokenWrapper>
      <SectionDivider />
      <TokenWrapper>
        <Header title="Grayscale" />
        <Row>
          <Swatch className="bg-grayscale-gray-50 text-text-primary" hex="#000000 50%" />
          <Swatch className="bg-grayscale-gray-20 text-text-primary" hex="#000000 20%" />
          <Swatch className="bg-grayscale-gray-10 text-text-primary" hex="#000000 10%" />
          <Swatch className="bg-grayscale-gray-5 text-text-primary" hex="#000000 5%" />
        </Row>
      </TokenWrapper>
    </>
  );
};

export default {
  title: "Foundation/Colors",
};
