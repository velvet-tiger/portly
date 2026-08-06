package classify

import "testing"

func TestFindProject(t *testing.T) {
	directories := fakeDirectories{present: map[string]bool{
		"/Users/dev/code/shop/.git":                 true,
		"/Users/dev/code/shop/api/package.json":     true,
		"/Users/dev/code/loose/nothing-here/marker": false,
	}}

	cases := []struct {
		name       string
		directory  string
		wantFound  bool
		wantName   string
		wantMarker string
	}{
		{
			name:       "the nearest marker wins, so a monorepo package beats its repository root",
			directory:  "/Users/dev/code/shop/api",
			wantFound:  true,
			wantName:   "api",
			wantMarker: "package.json",
		},
		{
			name:       "a nested directory walks up to the repository root",
			directory:  "/Users/dev/code/shop/src/components",
			wantFound:  true,
			wantName:   "shop",
			wantMarker: ".git",
		},
		{
			name:      "a directory with no marker above it is not a project",
			directory: "/Users/dev/code/loose/nothing-here",
			wantFound: false,
		},
		{
			name:      "a relative path is rejected rather than resolved against the cwd",
			directory: "code/shop",
			wantFound: false,
		},
		{
			name:      "an empty directory is not a project",
			directory: "",
			wantFound: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			project, found := FindProject(directories, testCase.directory)

			if found != testCase.wantFound {
				t.Fatalf("found = %v, want %v", found, testCase.wantFound)
			}
			if !found {
				return
			}
			if project.Name != testCase.wantName {
				t.Errorf("Name = %q, want %q", project.Name, testCase.wantName)
			}
			if project.Marker != testCase.wantMarker {
				t.Errorf("Marker = %q, want %q", project.Marker, testCase.wantMarker)
			}
		})
	}
}
