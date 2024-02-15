import { Api } from "./types";

const { api } = new Api();

// TODO: remove this when done with development
(window as any).api = api;

export { api };
