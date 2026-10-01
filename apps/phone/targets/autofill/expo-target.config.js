/** @type {import('@bacons/apple-targets/app.plugin').Config} */
module.exports = {
  type: "credentials-provider",
  name: "autofill",
  displayName: "Veil AutoFill",
  entitlements: {
    "com.apple.developer.authentication-services.autofill-credential-provider": true,
    "com.apple.security.application-groups": ["group.nyc.veil.phone"],
  },
};
