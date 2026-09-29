package config

import "testing"

func TestLoad(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local/db")
	c, err := Load()
	if err != nil || c.Workers != 2 || c.Runtime != "runsc" {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	t.Setenv("CJUDGE_WORKERS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("accepted zero workers")
	}
}
