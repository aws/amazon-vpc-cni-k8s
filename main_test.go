package main

import (
	"os"
	"testing"
)

func Test_copyFileContents(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{"Missing file", args{"missing", "out.txt"}, true},
		{"Copy executable", args{"./testdata/executable", "executable"}, false},
		{"Copy regular-file", args{"./testdata/regular-file.txt", "regular-file.txt"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := os.TempDir() + "/" + tt.args.dst

			err := copyFileContents(tt.args.src, dst)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("copyFileContents() error = %v, wantErr %v", err, tt.wantErr)
				}
			} else if !tt.wantErr {
				defer func() {
					e := os.Remove(dst)
					if e != nil {
						t.Errorf("Failed to clean up: %s", dst)
					}
				}()
				si, ei := os.Stat(tt.args.src)
				so, eo := os.Stat(dst)
				if ei != nil || eo != nil {
					t.Errorf("src error: %v, dst error: %v", ei, eo)
				} else if si.Mode() != so.Mode() {
					t.Errorf("File mode doesn't match! src: %v, dst: %v", si.Mode(), so.Mode())
				}
			}
		})
	}
}
