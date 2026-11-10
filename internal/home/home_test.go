package home

import (
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	abs := func(p string) string { a, _ := filepath.Abs(p); return a }
	tests := []struct {
		name, override, goos string
		env                  map[string]string
		want                 string
		wantErr              bool
	}{
		{"override wins", "x", "linux", map[string]string{"GSM_HOME": "/env"}, abs("x"), false},
		{"GSM_HOME", "", "linux", map[string]string{"GSM_HOME": "/env"}, abs("/env"), false},
		{"linux xdg", "", "linux", map[string]string{"XDG_DATA_HOME": "/xdg"}, filepath.Join("/xdg", "gsm"), false},
		{"linux default", "", "linux", nil, filepath.Join("/h", ".local", "share", "gsm"), false},
		{"darwin", "", "darwin", nil, filepath.Join("/h", "Library", "Application Support", "gsm"), false},
		{"windows", "", "windows", map[string]string{"LOCALAPPDATA": `C:\L`, "APPDATA": `C:\R`}, filepath.Join(`C:\L`, "gsm"), false},
		{"windows no localappdata", "", "windows", map[string]string{"APPDATA": `C:\R`}, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.override, tt.goos, func(k string) string { return tt.env[k] }, "/h")
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %q, %v; want %q (err %v)", got, err, tt.want, tt.wantErr)
			}
		})
	}
}
