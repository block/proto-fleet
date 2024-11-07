# Miner Web

## Overview

The source code of the mining dashboard UI is included here. Learn more about the tech stack [here](#learn-more).

## Directory Layout

```
miner-web
├── .storybook                # Storybook configuration, used for visual testing of UI components
├── dist                      # The compiled UI code for production, gets served by Actix web in miner-api-server
├── public                    # Includes the favicon image of the site (used for the browser tab's icon)
├── scripts                   # Automated scripts to help with development
├── src                       # Source for the mining dashboard UI
├── .gitignore                # Local files and folders to be ignored by git
├── README.md                 # This file
├── eslint.config.js          # Linter rules for maintaining standardization in the codebase
├── index.html                # Root file that gets served in browser
├── package.json              # Includes list of libraries used and npm command definitions
├── postcss.config.js         # Config file for postcss to add tailwind
├── tailwind.config.js        # Tailwind config file to extend css themes
├── tsconfig.json             # Typescript config file
└── vite.config.ts            # Vite configuration file, used for compiling the UI code
```

## Getting Started

### 1. Setup npm

Npm is needed to compile and run the UI code via vite. Instructions to install node and npm can be found [here](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm).

### 2. Install dependencies

```console
npm install
```

### 3. Start storybook

To visually test all of the components and see all the available styles in one place, run:

```console
npm run storybook
```

[localhost:6006](http://localhost:6006) should automatically open.

## Production build

For now we need to rebuild the UI production code manually through the below step. This may be converted to a github action in the future.

### 1. Compile the UI code for production

```console
  npm run build
```

## Test UI changes

To test the UI changes with the database and API, either copy the resulting `dist/` folder onto a mining device you've ssh'd to or follow the steps below to test locally.

There is a Yocto recipe `miner-web.bb` that copies the UI code compiled for production to the linux environment that gets served by Actix web in `main.rs` of `miner-api-server`.

### 1. Compile the UI code

```console
  npm run build
```

### 2. Build the linux image and bring it up
- Add and commit the changes to github
- Build the linux image via github actions
- Transfer the image to the SD card on the control board
- Connect the board via ethernet to your router
- Connect the board to your laptop

### 3. Access the UI
- Using tio connect to the board and find its IP address
- Enter the IP address into your browser to access the UI

## API typescript definition file

There is a `api/types.ts` file that has been automatically generated based on swagger's `MDK-API.json` file in `miner-api-server/docs`. To regenerate it, run this command:

```sh
node scripts/generate_api_ts.cjs
```

This file helps us maintain correct typing between the frontend and API and puts a wrapper around the API so we can make requests like `api.getNetwork().then(res => console.log(res))`.

To allow for easier development, `api` is exposed globally to enable making API calls in the browser's console.

## Learn More

To learn more about the tech stack, take a look at the following resources:

- [Learn React](https://react.dev/learn) - an interactive React tutorial.
- [Vite Documentation](https://vitejs.dev/guide/) - learn about Vite and its [list of community templates](https://github.com/vitejs/awesome-vite#templates). [template-vite-react](https://github.com/lzm0x219/template-vite-react) was used here.
- [Tailwind](https://tailwindcss.com/docs/utility-first) - learn about Tailwind and its features.
- [Recharts](https://release--63da8268a0da9970db6992aa.chromatic.com/?path=/docs/welcome--docs) - learn about the charting library.
