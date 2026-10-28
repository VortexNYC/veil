// Raise all pod targets' IPHONEOS_DEPLOYMENT_TARGET to the app floor —
// Xcode 26/27 rejects pods pinned below 15.0 (SDWebImage 9.0, Reachability 12.0).
const { withDangerousMod } = require("expo/config-plugins");
const fs = require("fs");
const path = require("path");

const FLOOR = "16.4";
const BLOCK = `
    installer.pods_project.targets.each do |target|
      target.build_configurations.each do |config|
        config.build_settings['IPHONEOS_DEPLOYMENT_TARGET'] = '${FLOOR}'
      end
    end
    if installer.respond_to?(:generated_projects)
      installer.generated_projects.each do |project|
        project.targets.each do |target|
          target.build_configurations.each do |config|
            config.build_settings['IPHONEOS_DEPLOYMENT_TARGET'] = '${FLOOR}'
          end
        end
      end
    end
`;

module.exports = function withPodDeploymentTarget(config) {
  return withDangerousMod(config, ["ios", async (c) => {
    const podfile = path.join(c.modRequest.platformProjectRoot, "Podfile");
    let s = fs.readFileSync(podfile, "utf8");
    if (!s.includes("generated_projects")) {
      s = s.replace(/(post_install do \|installer\|)/, `$1${BLOCK}`);
      fs.writeFileSync(podfile, s);
    }
    return c;
  }]);
};
