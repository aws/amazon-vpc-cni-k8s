// +build !windows

// Copyright 2014-2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package statemanager

import (
	"io/ioutil"
	"os"
	"path/filepath"
)

/*
On Linux, the basic approach for attempting to ensure that the state file is
written out correctly relies on behaviors of Linux and the ext* family of
filesystems.

On each save, the agent creates a new temporary file where it
writes out the json object.  Once the file is written, it gets renamed to the
well-known name of the state file.  Under the assumption of Linux + ext*, this
is an atomic operation; rename is changing the hard link of the well-known file
to point to the inode of the temporary file.  The original file inode now has no
links, and is considered free space once the opened file handles to that inode
are closed.

On each load, the agent opens a well-known file name for the state file and
reads it.
*/

func newPlatformDependencies() platformDependencies {
	return nil
}

func (manager *basicStateManager) readFile() ([]byte, error) {
	// Note that even if Save overwrites the file we're looking at here, we
	// still hold the old inode and should read the old data so no locking is
	// needed (given Linux and the ext* family of fs at least).
	file, err := os.Open(filepath.Join(manager.statePath, ecsDataFile))
	if err != nil {
		if os.IsNotExist(err) {
			// Happens every first run; not a real error
			return nil, nil
		}
		return nil, err
	}
	return ioutil.ReadAll(file)
}

func (manager *basicStateManager) writeFile(data []byte) error {
	// Make our temp-file on the same volume as our data-file to ensure we can
	// actually move it atomically; cross-device renaming will error out.
	tmpfile, err := ioutil.TempFile(manager.statePath, "tmp_ecs_agent_data")
	if err != nil {
		log.Error("Error saving state; could not create temp file to save state", "err", err)
		return err
	}
	_, err = tmpfile.Write(data)
	if err != nil {
		log.Error("Error saving state; could not write to temp file to save state", "err", err)
		return err
	}
	err = os.Rename(tmpfile.Name(), filepath.Join(manager.statePath, ecsDataFile))
	if err != nil {
		log.Error("Error saving state; could not move to data file", "err", err)
	}
	return err
}
