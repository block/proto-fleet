# Miner Web

## Overview

The source code of the mining dashboard UI is included here. For ease of development in non-linux environments, there is a docker compose file to bring up the needed DB and API. Alternatively, the swagger server can be used as a proxy for the API. Learn more about the tech stack [here](#learn-more).

## Directory Layout

```
miner-web
├── .storybook                # Storybook configuration, used for visual testing of UI components
├── dist                      # The compiled UI code for production, gets served by Actix web in miner-api-server
├── public                    # Includes the favicon image of the site (used for the browser tab's icon)
├── scripts                   # Automated scripts to help with development
├── src                       # Source for the mining dashboard UI
├── README.md                 # This file
├── docker-compose.yml        # Brings up the DB and API in docker containers
├── index.html                # Root file that gets served in browser
├── package.json              # Includes list of libraries used and npm command definitions
└── vite.config.ts            # Vite configuration file, used for compiling the UI code
```

## Getting Started

### Non-linux development environment

- First time building the UI?
  - See the setup guide: [Setup npm](#setup-npm)
- First time building the DB and API?
  - See the setup guide: [Setup docker](https://github.com/btc-mining/miner-firmware#setup-docker)


#### 1. Install all dependencies needed by the UI:

```console
npm install
```

#### 2. Start the backend services in docker or use swagger

<details>
  <summary>Docker</summary>

  - Stop any previous docker containers and start the DB and API ([details of what docker compose does](#docker-compose-details)):

    ```console
    docker compose down && docker compose up
    ```

    (Debug note: `miner-api-server` may try to access the DB file before `mcdd` is finished building it and throw an error. In those cases, make an arbitrary change such as adding a whitespace to a `miner-api-server` rust file and hit save to force it to rebuild successfully.)
</details>

<details>
  <summary>Swagger</summary>

  - To avoid running docker, set the api proxy to the swagger server by making this change to ```vite.config.ts```:

    ```
    -  "/api": "http://127.0.0.1:8080",
    +  "/api": "https://virtserver.swaggerhub.com/KSHITIZ_1/MDK-API/1.0.0",
    ```

</details>

#### 3. Start the frontend dev server

```console
npm run dev
```

Open [http://localhost:5173](http://localhost:5173) to see the results.

#### 4. Start storybook (optional)

To visually test all of the components and see all the available styles in one place, run:

```console
npm run storybook
```

[http://localhost:6006](http://localhost:6006) should automatically open.

## API typescript definition file

There is a `api/types.ts` file that has been automatically generated based on swagger's `MDK-API.json` file in `miner-www`. To regenerate it, run this command:

```sh
node scripts/generate_api_ts.cjs
```

This file helps us maintain correct typing between the frontend and API and puts a wrapper around the API so we can make requests like `api.getNetwork().then(res => console.log(res))`.

To allow for easier API development, `api` is exposed globally to enable making such calls in the browser's console. docker compose also runs `miner-api-server` in watch mode so that any saved changes to its rust files automatically trigger a rebuild.

## Production build

For now we need to rebuild the UI production code manually through the below steps. This may be converted to a github action in the future.

- First time building the UI?
  - See the setup guide: [Setup npm](#setup-npm)

#### 1. Compile the UI code for production

```console
  npm run build
```

There is a Yocto recipe `miner-web.bb` that copies the UI code compiled for production to the linux environment that gets served by Actix web in `miner-api-server/main.rs`.

#### 2. Build the linux image and bring it up
- Add and commit the changes to github
- Build the linux image via github actions
- Transfer the image to the SD card on the board
- Connect the board via ethernet to your router
- Connect the board to your laptop

#### 3. Access the UI
- Using tio connect to the board and find its IP address
- Enter the IP address into your browser to access the UI

## Prerequisites for building the UI

### 1. Setup npm

Npm is needed to compile and run the UI code via vite.
- Instructions to install node and npm can be found [here](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm).

### 2. Install UI dependencies

  ```console
  npm install
  ```

## Docker compose details

`docker-compose.yml`:
- brings up a bitcoin-core regtest-node
- brings up ckpool (edit the mining difficulty in `local-testchain/ckpool.conf`).
- brings up mcdd, cgminer, and miner-api-server in watch mode
    - executes the `scripts/start_mcdd_cgminer_miner_api_server.sh` script to build and run cgminer, mcdd, and miner-api-server

## Learn More

To learn more about the tech stack, take a look at the following resources:

- [Learn React](https://react.dev/learn) - an interactive React tutorial.
- [Vite Documentation](https://vitejs.dev/guide/) - learn about Vite and its [list of community templates](https://github.com/vitejs/awesome-vite#templates). [template-vite-react](https://github.com/lzm0x219/template-vite-react) was used here.
- [Tailwind](https://tailwindcss.com/docs/utility-first) - learn about Tailwind and its features.
