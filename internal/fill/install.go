package fill

import (
	"os"
	"path/filepath"
)

func Install(bin, vaultHome, userHome string) error {
	return InstallOrigin(InstallEnv{Bin: bin, VaultHome: vaultHome, UserHome: userHome})
}

func InstallOrigin(env InstallEnv) error {
	if err := os.MkdirAll(env.VaultHome, 0o700); err != nil {
		return err
	}
	host := HostPath(env.VaultHome)
	if err := copyExecutable(env.Bin, host); err != nil {
		return err
	}
	cfg := HostConfig{Origin: env.Origin, Home: env.VaultHome}
	if env.Origin != "" {
		cfg.TokenFile = filepath.Join(env.UserHome, ".config/veil/human.jwt")
		cfg.LoginEmail = env.LoginEmail
		cfg.PasswordFile = env.PasswordFile
		cfg.TOTPFile = env.TOTPFile
	}
	if err := WriteHostConfig(env.VaultHome, cfg); err != nil {
		return err
	}
	chromeDir := filepath.Join(env.UserHome, "Library/Application Support/Google/Chrome/NativeMessagingHosts")
	ffDir := filepath.Join(env.UserHome, "Library/Application Support/Mozilla/NativeMessagingHosts")
	if err := os.MkdirAll(chromeDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(ffDir, 0o755); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(chromeDir, NativeHostName+".json"))
	_ = os.Remove(filepath.Join(ffDir, NativeHostName+".json"))
	if err := os.WriteFile(filepath.Join(chromeDir, JSONHostName+".json"), ManifestJSONChrome(host), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(ffDir, JSONHostName+".json"), ManifestJSONFirefox(host), 0o644)
}
