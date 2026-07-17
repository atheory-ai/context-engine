#!/usr/bin/env node
"use strict";

const { execFileSync } = require("child_process");
const { binaryPath } = require("../lib/install.js");

const binary = binaryPath();

try {
  execFileSync(binary, process.argv.slice(2), {
    stdio: "inherit",
    windowsHide: false,
  });
} catch (error) {
  process.exit(error.status ?? 1);
}
