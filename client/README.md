# Miner Web

## Overview

The source code of the mining dashboard UI (ProtoOS) and the fleet management UI (protoFLeet) is included here. Learn more about the tech stack [here](#learn-more).

## Directory Layout

```
client
├── .storybook                # Storybook configuration, used for visual testing of UI components
├── dist                      # The compiled UI code for production
│  ├── protoFleet             # The compile ProtoFleet app, gets served by TBD
│  └── protoOS                # The compile ProtoOS app, gets served by Actix web in miner-api-server
├── public                    # Includes the favicon image of the site (used for the browser tab's icon)
├── scripts                   # Automated scripts to help with development
├── src                       # Source for the mining dashboard UI and fleet management UI
│  ├── protoFleet             # Source for the fleet management UI
│  │  └── index.html          # Root file that gets served in browser for protoFLeet
│  ├── protoOS                # Source for the mining dashboard UI
│  │  └── index.html          # Root file that gets served in browser for protoOS
│  └── shared                 # Shared source used by ProtoOS and ProtoFleet
├── .gitignore                # Local files and folders to be ignored by git
├── README.md                 # This file
├── eslint.config.js          # Linter rules for maintaining standardization in the codebase
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

```bash
  # build both ProtoOS & ProtoFleet artifacts
  npm run build

    # build protoOS
  npm run build:protoOS

    # build protoFleet
  npm run build:protoFleet
```

### 2. Preview production build

```bash
  # preview protosOS production build
  npm run preview:protoOS

  # preview protosFleet production build
  npm run preview:protoFleet
```

## Dev build

To test code changes locally, run the vite dev server with one of the following commands.

### 1. Setup Proxies

**ProtoOS**

- In production the ProtoOS frontend is served from same host as the api, to run this locally we must set up a proxy to route all api requests to another server.
- Create an `.env` file in the root of this directory and define an environment variable called `PROXY_URL`. This file is ignored by git. The proxy url could be anywhere that the MDK api is served. Some options may be:
  - The URL of a miner-api-server running locally (ie. `http://127.0.0.1:8000`)
  - An IP of one of the [test nodes in the lab](https://www.notion.so/proto-team/Test-Nodes-go-prototestnodes-4ec0b2eb74064ab8a7166cfe68ece300)
  - Mock data API server like [stoplight](https://stoplight.io/mocks/proto-mining/mdk-api/656299768)

```
PROXY_URL = http://127.0.0.1:8000
```

**ProtoFleet**

- Build and start the docker containers to run the backend locally
  - `cd ../server`
  - `docker-compose down --rmi all --volumes && docker-compose up --build -d` // stops all current containers and removes volumes, builds fresh images and starts all containers in the background
  - Now you will have the server running, mysql running and 2 or more miner emulators running locally
- Because of CORS we cannot makes request directly from our dev server to the docker backend. Instead specify your docker server url in `.env` file. `FLEET_PROXY_URL="http://localhost:4000"`. Vite will proxy all api paths listed in `vite.config.ts` to the protoFleet server

```
FLEET_PROXY_URL = http://127.0.0.1:4000
```

- If you are implementing a new API endpoint you may need to add the path to the `vite.config.ts`

### 2. Start dev server

```bash
  # start protoOS dev server
  npm run dev:protoOS

  # start protoFleet dev server
  npm run dev:protoFleet
```

### 3. Access the UI

Enter vite server url in browser `http://localhost:5173`

## Testing locally on hardware

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

There is a `/protoOS/api/types.ts` file that has been automatically generated based on the `MDK-API.json` OpenAPI spec in `proto-rig-api/openapi/`.

Run this command to regenerate the file:

```sh
node scripts/generate_api_ts.mjs
```

This file helps us maintain correct typing between the frontend and API and puts a wrapper around the API so we can make requests like `api.getNetwork().then(res => console.log(res))`.

To allow for easier development, `api` is exposed globally to enable making API calls in the browser's console.

## Learn More

To learn more about the tech stack, take a look at the following resources:

- [Learn React](https://react.dev/learn) - an interactive React tutorial.
- [Vite Documentation](https://vitejs.dev/guide/) - learn about Vite and its [list of community templates](https://github.com/vitejs/awesome-vite#templates). [template-vite-react](https://github.com/lzm0x219/template-vite-react) was used here.
- [Tailwind](https://tailwindcss.com/docs/utility-first) - learn about Tailwind and its features.
- [Recharts](https://release--63da8268a0da9970db6992aa.chromatic.com/?path=/docs/welcome--docs) - learn about the charting library.
