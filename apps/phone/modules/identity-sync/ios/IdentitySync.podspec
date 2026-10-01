require 'json'

package = JSON.parse(File.read(File.join(__dir__, '..', 'package.json')))

Pod::Spec.new do |s|
  s.name           = 'IdentitySync'
  s.version        = package['version']
  s.summary        = 'Veil AutoFill identity sync'
  s.description    = 'Publishes the credential handoff and QuickType identities for the Veil AutoFill appex'
  s.license        = 'MIT'
  s.author         = 'Vortex'
  s.homepage       = 'https://veil.nyc'
  s.platforms      = { :ios => '15.1' }
  s.source         = { git: '' }
  s.static_framework = true

  s.dependency 'ExpoModulesCore'

  s.pod_target_xcconfig = {
    'DEFINES_MODULE' => 'YES',
    'SWIFT_COMPILATION_MODE' => 'wholemodule'
  }

  s.source_files = "**/*.{h,m,mm,swift,hpp,cpp}"
end
