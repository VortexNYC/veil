package cli

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	_ = os.Unsetenv("VEIL_HYDRA_ISSUER")
	_ = os.Unsetenv("VEIL_HYDRA_ADMIN")
	_ = os.Unsetenv("VEIL_HOME")
	_ = os.Unsetenv("VEIL_OIDC_TOKEN")
	_ = os.Unsetenv("VEIL_OIDC_TOKEN_FILE")
	_ = os.Unsetenv("VEIL_ORIGIN")
	_ = os.Unsetenv("VEIL_HYDRA_SECRET_FILE")
	_ = os.Unsetenv("VEIL_HUMAN_TOKEN")
	_ = os.Unsetenv("VEIL_HUMAN_TOKEN_FILE")
	_ = os.Unsetenv("VEIL_LOGIN_NO_OPEN")
	_ = os.Unsetenv("VEIL_LOGIN_EMAIL")
	_ = os.Unsetenv("VEIL_KRATOS_PASSWORD_FILE")
	_ = os.Unsetenv("VEIL_KRATOS_TOTP_FILE")
	_ = os.Setenv("VEIL_FILL_TOUCHID", "0")
	_ = os.Setenv("VEIL_DPOP", "0")
	os.Exit(m.Run())
}
