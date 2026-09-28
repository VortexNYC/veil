const { withUniwindConfig } = require("uniwind/metro");
const { getDefaultConfig } = require("expo/metro-config");

/** @type {import('expo/metro-config').MetroConfig} */
const config = getDefaultConfig(__dirname);

// withUniwindConfig must be the outermost wrapper
module.exports = withUniwindConfig(config, {
  cssEntryFile: "./global.css",
  inlineRem: 16,
});
