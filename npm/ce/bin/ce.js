#!/usr/bin/env node
"use strict";

const { execFileSync } = require("child_process");
const path = require("path");

const PLATFORM_PACKAGES = {
  "darwin-arm64": "@atheory-ai/ce-darwin-arm64",
  "darwin-x64": "@atheory-ai/ce-darwin-x64",
  "linux-x64": "@atheory-ai/ce-linux-x64",
  "linux-arm64": "@atheory-ai/ce-linux-arm64",
  "win32-x64": "@atheory-ai/ce-win32-x64",
  "win32-arm64": "@atheory-ai/ce-win32-arm64",
};

function getBinaryPath() {
  const key = `${process.platform}-${process.arch}`;
  const packageName = PLATFORM_PACKAGES[key];

  if (!packageName) {
    throw new Error(
      `ce: unsupported platform "${key}"\n` +
        `Supported platforms: ${Object.keys(PLATFORM_PACKAGES).join(", ")}\n` +
        "\nPlease open an issue at https://github.com/atheory-ai/context-engine/issues",
    );
  }

  let packageDir;
  try {
    packageDir = path.dirname(require.resolve(`${packageName}/package.json`));
  } catch {
    throw new Error(
      `ce: the platform package "${packageName}" is not installed.\n` +
        "\nThis usually happens when optional dependencies were skipped. Try:\n" +
        "  npm install --include=optional\n" +
        "  pnpm install\n" +
        "  yarn install",
    );
  }

  return path.join(packageDir, "bin", process.platform === "win32" ? "ce.exe" : "ce");
}

let binaryPath;
try {
  binaryPath = getBinaryPath();
} catch (err) {
  process.stderr.write(`${err.message}\n`);
  process.exit(1);
}

try {
  execFileSync(binaryPath, process.argv.slice(2), {
    stdio: "inherit",
    windowsHide: false,
  });
} catch (err) {
  process.exit(err.status ?? 1);
}
