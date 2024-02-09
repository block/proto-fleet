#!/usr/bin/env node

const fs = require("fs");
const path = require("path");
const {
  generateApi,
} = require("swagger-typescript-api/src");

const swaggerSchemaPath = path.resolve(
  __dirname,
  "../../miner-www/api/MDK-API.json"
);

if (!fs.existsSync(swaggerSchemaPath)) {
  console.error(`\nCould not find Swagger Schema file: ${swaggerSchemaPath}\n`);
  process.exitCode = 1;
}

const [fileName = "Api.ts"] = process.argv.slice(2);
const fileDir = path.resolve(__dirname, "../src");

generateApi({
  name: fileName,
  input: swaggerSchemaPath,
  output: fileDir,
  addReadonly: true,
  httpClientType: "fetch",
  sortTypes: true,
}).then(() => {
  const filePath = path.join(fileDir, fileName);
  let fileContent = fs.readFileSync(filePath, "utf-8");

  fileContent = fileContent.replace(
    /public baseUrl: string = ".*";/g,
    'public baseUrl: string = "";'
  );

  fs.writeFileSync(filePath, fileContent, "utf-8");
});
