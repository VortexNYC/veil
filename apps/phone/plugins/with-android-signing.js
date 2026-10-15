// Injects the Play upload-key signing config into the generated
// app/build.gradle on prebuild so release builds sign durably.
const { withAppBuildGradle } = require("expo/config-plugins");

function transform(contents) {
  return contents
    .replace(
      "    signingConfigs {\n        debug {",
      `    signingConfigs {
        release {
            storeFile file("../../keystores/veil-upload.keystore")
            keyAlias "veil-upload"
            storePassword System.getenv("VEIL_UPLOAD_STORE_PASSWORD") ?: "veil-upload-2026"
            keyPassword System.getenv("VEIL_UPLOAD_KEY_PASSWORD") ?: "veil-upload-2026"
        }
        debug {`,
    )
    .replace(
      /(release \{[\s\S]*?)signingConfig signingConfigs\.debug/,
      "$1signingConfig signingConfigs.release",
    );
}
module.exports = function withAndroidSigning(config) {
  return withAppBuildGradle(config, (cfg) => {
    cfg.modResults.contents = transform(cfg.modResults.contents);
    return cfg;
  });
};
module.exports.transform = transform;
