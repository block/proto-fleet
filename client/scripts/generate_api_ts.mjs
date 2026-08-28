#!/usr/bin/env node

import fs from "fs";
import path from "path";
import { generateApi } from "swagger-typescript-api";
import { fileURLToPath } from "url";

const __filename = fileURLToPath(import.meta.url); // get the resolved path to the file
const __dirname = path.dirname(__filename); // get the name of the directory

// Each generated MDK client is a private implementation detail of its adapter.
// Keep the OpenAPI sources versioned side by side and emit each under
// src/protoOS/api/adapters/<dialect>/generated/. The eslint `no-restricted-imports`
// boundary forbids anything outside an adapter from importing its generated/ folder.
const targets = [
  {
    dialect: "mdkV1",
    schema: path.resolve(__dirname, "../../proto-rig-api/openapi/MDK-API.json"),
    fileName: "generatedApi.ts",
  },
  // Future v2 schema (#33) emits here once implemented:
  // {
  //   dialect: "mdkV2",
  //   schema: path.resolve(__dirname, "../../proto-rig-api/openapi/MDK-v2-API.json"),
  //   fileName: "generatedApi.ts",
  // },
];

const generateTarget = async ({ dialect, schema, fileName }) => {
  if (!fs.existsSync(schema)) {
    console.error(`\nCould not find Swagger Schema file: ${schema}\n`);
    process.exitCode = 1;
    return;
  }

  const output = path.resolve(
    __dirname,
    `../src/protoOS/api/adapters/${dialect}/generated`,
  );

  await generateApi({
    fileName,
    input: schema,
    extractRequestParams: true,
    output,
    addReadonly: true,
    httpClientType: "fetch",
    sortTypes: true,
  });

  const filePath = path.join(output, fileName);
  let fileContent = fs.readFileSync(filePath, "utf-8");

  fileContent = fileContent.replace(
    /public baseUrl: string = ".*";/g,
    'public baseUrl: string = "";',
  );

  fs.writeFileSync(filePath, fileContent, "utf-8");
  console.log(`Generated ${dialect} client → ${path.relative(process.cwd(), filePath)}`);
};

for (const target of targets) {
  await generateTarget(target);
}
