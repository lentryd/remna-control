package config

import "testing"

func TestLoad_PanelSync(t *testing.T) {
	t.Setenv("REMNAWAVE_URL", "http://panel")
	t.Setenv("REMNAWAVE_TOKEN", "tok")
	t.Setenv("JWT_SECRET", "secret")

	for _, v := range []string{"0s", "-5m"} {
		t.Setenv("PANEL_SYNC_INTERVAL", v)
		if _, err := Load(); err == nil {
			t.Errorf("PANEL_SYNC_INTERVAL=%s: want an error", v)
		}
	}

	t.Setenv("PANEL_SYNC_INTERVAL", "1m")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
