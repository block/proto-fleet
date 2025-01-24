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

### Testing local dev build against test node in lab

#### 1. Locate test node IP

- https://www.notion.so/proto-team/Test-Nodes-go-prototestnodes-4ec0b2eb74064ab8a7166cfe68ece300

#### 2. Start Dev server and API Server Proxy

```console
  npm run devproxy --proxyUrl <test_node_IP>
```

#### 3. Access UI

- enter vite server url in browser `http://localhost:5173`

### Testing with CPU mining running on laptop

#### 1. Compile the UI code

```console
  npm run build
```

#### 2. Run `mcdd`and `miner-api-server` on laptop

```console
cd ~/Development/miner-firmware/crates/mcdd && cargo run --release
cd ~/Development/miner-firmware/crates/miner-api-server && cargo run -- --www-path="../../miner-web/dist"
```

#### 3. Access the UI

- enter the the local ip address that miner-api-server is running on `http://127.0.0.1:8080`
- alternatively run `npm run devproxy --proxyUrl http://127.0.0.1:8080` to start Vite server and proxy all api requests to `miner-api-server`

#### 4. Gotchas

- When first visiting the UI you will need to onboard and [add mining pool](https://www.notion.so/proto-team/How-to-connect-to-Block-s-pool-and-wallet-for-live-network-testing-db54d1cd5d2d4cc59bf68b8623da4c61). However after completing this step the UI will just return back to the pools view and appear like no pool was added.
- To get past this you must change the following snippet in `miner-firmware/crates/miner-api-server/controllers/system.rs` ln 177

```rust
  HttpResponse::Ok().json(SystemStatuses {
      // onboarded: Some(get_onboarded_status()),
      onboarded: Some(true),
      password_set: Some(password_status),
  })
```

- Restart the `miner-api-server` after making this change and you should be able to access the onboarded state of the UI

### Testing locally on hardware

There is a Yocto recipe `miner-web.bb` that copies the UI code compiled for production to the linux environment that gets served by Actix web in `main.rs` of `miner-api-server`.

#### 1. Compile the UI code

```console
  npm run build
```

#### 2. Build the linux image and bring it up

- Add and commit the changes to github
- Build the linux image via github actions
- Transfer the image to the SD card on the control board
- Connect the board via ethernet to your router
- Connect the board to your laptop

#### 3. Access the UI

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
