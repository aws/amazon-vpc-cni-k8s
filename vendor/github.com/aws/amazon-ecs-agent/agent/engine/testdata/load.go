package testdata

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"runtime"

	"github.com/aws/amazon-ecs-agent/agent/api"
)

func LoadTask(name string) *api.Task {
	_, filename, _, _ := runtime.Caller(0)
	filedata, err := ioutil.ReadFile(filepath.Join(filepath.Dir(filename), "test_tasks", name+".json"))
	if err != nil {
		panic(err)
	}
	t := &api.Task{}
	if err := json.Unmarshal(filedata, t); err != nil {
		panic(err)
	}
	return t
}
