#!/usr/bin/env node

import fs from "fs";
import path from "path";
import { generateApi } from "swagger-typescript-api";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url); // get the resolved path to the file
const __dirname = path.dirname(__filename); // get the name of the directory

const swaggerSchemaPath = path.resolve(
  __dirname,
  "../../proto-rig-api/openapi/MDK-API.json",
);

if (!fs.existsSync(swaggerSchemaPath)) {
  console.error(`\nCould not find Swagger Schema file: ${swaggerSchemaPath}\n`);
  process.exitCode = 1;
}

const [fileName = "generatedApi.ts"] = process.argv.slice(2);
const fileDir = path.resolve(__dirname, "../src/protoOS/api");

generateApi({
  fileName,
  input: swaggerSchemaPath,
  extractRequestParams: true,
  output: fileDir,
  addReadonly: true,
  httpClientType: "fetch",
  sortTypes: true,
}).then(() => {
  const filePath = path.join(fileDir, fileName);
  let fileContent = fs.readFileSync(filePath, "utf-8");

  fileContent = fileContent.replace(
    /public baseUrl: string = ".*";/g,
    'public baseUrl: string = "";',
  );

  fs.writeFileSync(filePath, fileContent, "utf-8");
});
