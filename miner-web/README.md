# Web

This folder is for the frontend side of the mining tool. For ease of development in non-linux env, there is a docker compose file to bring up the needed DB and API.

## Prerequisites

- Make sure you have npm installed:

  ```sh
  npm -v
  ```
  Follow the steps [here](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) to install Node and npm if needed.
- Install dependencies:

  ```console
  npm install
  ```

## Deploy

To regenerate the `dist/` folder, run:

```console
npm run build
```

## Run the development server

Start the backend services locally:

```console
docker compose up
```

Start the frontend dev server:

```console
npm run dev
```

Open [http://localhost:5173](http://localhost:5173) with your browser to see the results.

## API typescript definition file

There is a `Api.ts` file that has been automatically generated based on the swagger json. To regenerate it, run this command:

```sh
node scripts/generate_api_ts.js
```

This file helps us maintain correct typing between the frontend and API and puts a wrapper around the API so we can make requests like `api.network().then(res => {})`.

## Storybook

To easily see all of the components and available styles, run:

```console
npm run storybook
```

## To directly access MCDD DB:
1. ```docker exec -it mcdd bash```
2. ```sqlite3 /dev/shm/mcdd.sqlite```
3. ```.tables``` to see a list of all tables
4. ```select * from miner_components;``` etc

(DB only is available while mcdd is running. if DB file does not exist, check `top` to make sure mcdd running.)

## To make an API request to MCDD:
1. ```docker exec -it mcdd bash```
2. ```curl 127.0.0.1:2121/api/v1```

## To make an API request to miner-api-server:
1. ```docker exec -it miner-api-server bash```
2. ```curl 127.0.0.1:8080/api/v1```

## Details

`docker-compose.yml`:
- brings up a bitcoin-core regtest-node
- brings up ckpool (edit the mining difficulty in `local-testchain/ckpool.conf`).
- brings up mcdd
    - executes the `scripts/start_mcdd_cgminer.sh` script to build and run cgminer and mcdd
- brings up the miner-api-server in watch mode

## Learn More

To learn more about the tech stack, take a look at the following resources:

- [Learn React](https://react.dev/learn) - an interactive React tutorial.
- [Vite Documentation](https://vitejs.dev/guide/) - learn about Vite and its [list of community templates](https://github.com/vitejs/awesome-vite#templates). [template-vite-react](https://github.com/lzm0x219/template-vite-react) was used here.
- [Tailwind](https://tailwindcss.com/docs/utility-first) - learn about Tailwind and its features.
