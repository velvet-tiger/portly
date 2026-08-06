package release

import (
	"runtime/debug"
	"testing"
)

func TestDescribe(t *testing.T) {
	cases := []struct {
		name          string
		linkerVersion string
		linkerCommit  string
		info          *debug.BuildInfo
		want          string
	}{
		{
			name:          "a release build reports its tag and short commit",
			linkerVersion: "1.2.0",
			linkerCommit:  "9f3c1ab4d5e6f70123456789",
			want:          "portly 1.2.0 (9f3c1ab)",
		},
		{
			name:          "a go install build falls back to the module version",
			linkerVersion: DevelopmentVersion,
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "v0.1.0"},
				Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abcdef1234567890"}},
			},
			want: "portly v0.1.0 (abcdef1)",
		},
		{
			name:          "a working tree build is not passed off as a release",
			linkerVersion: DevelopmentVersion,
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "1111111222222"}},
			},
			want: "portly dev (1111111)",
		},
		{
			name:          "a bare local build reports dev with no commit",
			linkerVersion: DevelopmentVersion,
			info:          nil,
			want:          "portly dev",
		},
		{
			name:          "an empty linker version is treated as no version at all",
			linkerVersion: "",
			info:          nil,
			want:          "portly dev",
		},
		{
			name:          "a short commit is left alone rather than truncated further",
			linkerVersion: "1.0.0",
			linkerCommit:  "abc123",
			want:          "portly 1.0.0 (abc123)",
		},
		{
			name:          "module metadata without any revision still yields a version",
			linkerVersion: DevelopmentVersion,
			info:          &debug.BuildInfo{Main: debug.Module{Version: "v0.2.0"}},
			want:          "portly v0.2.0",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := Describe(testCase.linkerVersion, testCase.linkerCommit, testCase.info).String()
			if got != testCase.want {
				t.Errorf("Describe().String() = %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestDescribePrefersTheLinkerVersionOverModuleMetadata(t *testing.T) {
	build := Describe("2.0.0", "feedface0000", &debug.BuildInfo{
		Main: debug.Module{Version: "v0.1.0"},
	})

	if build.Version != "2.0.0" {
		t.Errorf("Version = %q, want the linker value 2.0.0", build.Version)
	}
}
