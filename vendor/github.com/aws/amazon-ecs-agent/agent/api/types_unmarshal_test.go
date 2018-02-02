package api

import (
	"encoding/json"
	"testing"
)

func TestVolumesFromUnmarshal(t *testing.T) {
	var vols []VolumeFrom
	err := json.Unmarshal([]byte(`[{"sourceContainer":"c1"},{"sourceContainer":"c2","readOnly":true}]`), &vols)
	if err != nil {
		t.Fatal("Unable to unmarshal json")
	}
	if (vols[0] != VolumeFrom{SourceContainer: "c1", ReadOnly: false}) {
		t.Error("VolumeFrom 1 didn't match expected output")
	}
	if (vols[1] != VolumeFrom{SourceContainer: "c2", ReadOnly: true}) {
		t.Error("VolumeFrom 2 didn't match expected output")
	}
}

func TestEmptyHostVolumeUnmarshal(t *testing.T) {
	var task Task
	err := json.Unmarshal([]byte(`{"volumes":[{"name":"test","host":{}}]}`), &task)
	if err != nil {
		t.Fatal("Could not unmarshal: ", err)
	}
	if task.Volumes[0].Name != "test" {
		t.Error("Wrong name")
	}
	if fs, ok := task.Volumes[0].Volume.(*EmptyHostVolume); !ok {
		t.Error("Wrong type")
		if fs.SourcePath() != "" {
			t.Error("Should default to empty string")
		}
	}
}

func TestHostHostVolumeUnmarshal(t *testing.T) {
	var task Task
	err := json.Unmarshal([]byte(`{"volumes":[{"name":"test","host":{"sourcePath":"/path"}}]}`), &task)
	if err != nil {
		t.Fatal("Could not unmarshal: ", err)
	}
	if task.Volumes[0].Name != "test" {
		t.Error("Wrong name")
	}
	fsv, ok := task.Volumes[0].Volume.(*FSHostVolume)
	if !ok {
		t.Error("Wrong type")
	} else if fsv.SourcePath() != "/path" {
		t.Error("Wrong host path: ", fsv.SourcePath())
	}
}
