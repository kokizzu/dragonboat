// Copyright 2017-2020 Lei Ni (nilei81@gmail.com) and other contributors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package fileutil

import (
	"testing"

	"github.com/lni/dragonboat/v4/internal/vfs"
	"github.com/stretchr/testify/require"
)

func TestVFSSync(t *testing.T) {
	fs := vfs.GetTestFS()
	if fs == vfs.DefaultFS {
		t.Skip("not using memfs, skipped")
	}
	err := MkdirAll("/dragonboat-test-data/data", fs)
	require.NoError(t, err, "failed to mkdir")

	ffs, ok := fs.(*vfs.MemFS)
	require.True(t, ok, "not a memfs")

	ffs.ResetToSyncedState()

	ok, err = DirExist("/dragonboat-test-data", fs)
	require.NoError(t, err, "failed to check exist")
	require.True(t, ok, "test dir disappeared")
}
