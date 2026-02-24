package fzf

import (
	"testing"
)

func TestParseFZFVersion(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMajor int
		wantMinor int
		wantPatch int
		wantErr   bool
	}{
		{
			name:      "version with brew suffix",
			input:     "0.35.1 (brew)",
			wantMajor: 0,
			wantMinor: 35,
			wantPatch: 1,
		},
		{
			name:      "version with commit hash suffix",
			input:     "0.44.0 (e5765b3)",
			wantMajor: 0,
			wantMinor: 44,
			wantPatch: 0,
		},
		{
			name:      "version without suffix",
			input:     "0.40.0",
			wantMajor: 0,
			wantMinor: 40,
			wantPatch: 0,
		},
		{
			name:      "major version bump",
			input:     "1.0.0",
			wantMajor: 1,
			wantMinor: 0,
			wantPatch: 0,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "garbage input",
			input:   "not a version at all",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			major, minor, patch, err := ParseFZFVersion(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseFZFVersion(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if major != tt.wantMajor || minor != tt.wantMinor || patch != tt.wantPatch {
				t.Errorf("ParseFZFVersion(%q) = (%d, %d, %d), want (%d, %d, %d)",
					tt.input, major, minor, patch, tt.wantMajor, tt.wantMinor, tt.wantPatch)
			}
		})
	}
}

func TestIsVersionSufficient(t *testing.T) {
	tests := []struct {
		name  string
		major int
		minor int
		patch int
		want  bool
	}{
		{
			name:  "below minimum",
			major: 0,
			minor: 35,
			patch: 1,
			want:  false,
		},
		{
			name:  "exactly at minimum",
			major: 0,
			minor: 40,
			patch: 0,
			want:  true,
		},
		{
			name:  "above minimum minor",
			major: 0,
			minor: 44,
			patch: 0,
			want:  true,
		},
		{
			name:  "major version bump",
			major: 1,
			minor: 0,
			patch: 0,
			want:  true,
		},
		{
			name:  "same minor higher patch",
			major: 0,
			minor: 40,
			patch: 5,
			want:  true,
		},
		{
			name:  "below minimum minor zero patch",
			major: 0,
			minor: 39,
			patch: 99,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVersionSufficient(tt.major, tt.minor, tt.patch)
			if got != tt.want {
				t.Errorf("isVersionSufficient(%d, %d, %d) = %v, want %v",
					tt.major, tt.minor, tt.patch, got, tt.want)
			}
		})
	}
}
