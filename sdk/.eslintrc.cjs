module.exports = {
  root: true,
  extends: ["./.eslintrc.base.json"],
  env: {
    es2020: true,
    node: true,
  },
  parserOptions: {
    ecmaVersion: "latest",
    sourceType: "module",
  },
  ignorePatterns: [
    "**/dist/**",
    "**/node_modules/**",
    "coverage/**",
  ],
}