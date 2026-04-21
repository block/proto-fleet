/* eslint-disable react-refresh/only-export-components */
import React, { ComponentType, useEffect, useMemo } from "react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import type { Preview } from "@storybook/react-vite";
import "../src/shared/styles/index.css";

import { spyOn } from "storybook/test";

export const beforeEach = () => {
  spyOn(console, "log").mockName("console.log");
  spyOn(console, "warn").mockName("console.warn");
};

const ThemeWrapper = ({ theme, children }: { theme: string; children: React.ReactNode }) => {
  useEffect(() => {
    document.body.setAttribute("data-theme", theme);
  }, [theme]);
  return <>{children}</>;
};

const StoryRouter = ({ Story }: { Story: ComponentType }) => {
  const router = useMemo(() => createMemoryRouter([{ path: "*", element: <Story /> }]), [Story]);

  return <RouterProvider router={router} />;
};

export const decorators = [
  (Story: ComponentType, context: { globals: { theme?: string }; parameters: { withRouter?: boolean } }) => {
    const theme = context.globals.theme || "light";

    if (context.parameters.withRouter === false) {
      return (
        <ThemeWrapper theme={theme}>
          <Story />
        </ThemeWrapper>
      );
    }

    return (
      <ThemeWrapper theme={theme}>
        <StoryRouter Story={Story} />
      </ThemeWrapper>
    );
  },
];

const preview: Preview = {
  globalTypes: {
    theme: {
      description: "Theme",
      toolbar: {
        title: "Theme",
        icon: "mirror",
        items: [
          { value: "light", title: "Light", icon: "sun" },
          { value: "dark", title: "Dark", icon: "moon" },
        ],
        dynamicTitle: true,
      },
    },
  },
  initialGlobals: {
    theme: "light",
  },
  parameters: {
    actions: { argTypesRegex: "^on[A-Z].*" },
    layout: "fullscreen",
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    options: {
      storySort: {
        order: ["Foundation", "Shared", "ProtoOS", "Proto Fleet", "*"],
      },
    },
  },
};

export default preview;
