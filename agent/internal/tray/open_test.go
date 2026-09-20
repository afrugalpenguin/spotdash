package tray

import (
	"reflect"
	"testing"
)

// .log is often unassociated on Windows, so it names Notepad.
func TestLogCommandPicksAViewerPerOS(t *testing.T) {
	const path = `C:\Users\Some Name\spotdash.log`
	tests := []struct {
		goos     string
		wantName string
		wantArgs []string
	}{
		{"windows", "notepad.exe", []string{path}},
		{"darwin", "open", []string{"-t", path}},
		{"linux", "xdg-open", []string{path}},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			name, args := logCommand(tt.goos, path)

			if name != tt.wantName {
				t.Errorf("command = %q, want %q", name, tt.wantName)
			}
			if !reflect.DeepEqual(args, tt.wantArgs) {
				t.Errorf("args = %q, want %q", args, tt.wantArgs)
			}
		})
	}
}
