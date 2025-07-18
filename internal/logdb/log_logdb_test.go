// Copyright 2017-2020 Lei Ni (nilei81@gmail.com) and other contributors.
// Copyright 2015 The etcd Authors
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

package logdb

// This file contains tests ported from etcd raft

import (
	"math"
	"testing"

	"github.com/lni/dragonboat/v4/internal/raft"
	"github.com/lni/dragonboat/v4/internal/vfs"
	pb "github.com/lni/dragonboat/v4/raftpb"
	"github.com/stretchr/testify/require"
)

func removeTestLogdbDir(fs vfs.IFS) {
	if err := fs.RemoveAll(RDBTestDirectory); err != nil {
		panic(err)
	}
}

func getTestLogReaderWithoutCache(entries []pb.Entry) *LogReader {
	logdb := getNewLogReaderTestDB(entries, vfs.GetTestFS())
	ls := NewLogReader(LogReaderTestShardID, LogReaderTestReplicaID, logdb)
	ls.SetCompactor(testCompactor)
	if len(entries) > 0 {
		if err := ls.Append(entries); err != nil {
			panic(err)
		}
	}
	return ls
}

func TestRLLTFindConflict(t *testing.T) {
	previousEnts := []pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2},
		{Index: 3, Term: 3}}
	tests := []struct {
		ents      []pb.Entry
		wconflict uint64
	}{
		// no conflict, empty ent
		{[]pb.Entry{}, 0},
		// no conflict
		{[]pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2},
			{Index: 3, Term: 3}}, 0},
		{[]pb.Entry{{Index: 2, Term: 2}, {Index: 3, Term: 3}}, 0},
		{[]pb.Entry{{Index: 3, Term: 3}}, 0},
		// no conflict, but has new entries
		{[]pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2},
			{Index: 3, Term: 3}, {Index: 4, Term: 4},
			{Index: 5, Term: 4}}, 4},
		{[]pb.Entry{{Index: 2, Term: 2}, {Index: 3, Term: 3},
			{Index: 4, Term: 4}, {Index: 5, Term: 4}}, 4},
		{[]pb.Entry{{Index: 3, Term: 3}, {Index: 4, Term: 4},
			{Index: 5, Term: 4}}, 4},
		{[]pb.Entry{{Index: 4, Term: 4}, {Index: 5, Term: 4}}, 4},
		// conflicts with existing entries
		{[]pb.Entry{{Index: 1, Term: 4}, {Index: 2, Term: 4}}, 1},
		{[]pb.Entry{{Index: 2, Term: 1}, {Index: 3, Term: 4},
			{Index: 4, Term: 4}}, 2},
		{[]pb.Entry{{Index: 3, Term: 1}, {Index: 4, Term: 2},
			{Index: 5, Term: 4}, {Index: 6, Term: 4}}, 3},
	}
	stable := getTestLogReaderWithoutCache(previousEnts)
	defer removeTestLogdbDir(vfs.GetTestFS())
	defer func() {
		require.NoError(t, stable.logdb.(*ShardedDB).Close())
	}()
	for i, tt := range tests {
		raftLog := raft.NewLog(stable)
		gconflict, err := raftLog.GetConflictIndex(tt.ents)
		require.NoError(t, err)
		require.Equal(t, tt.wconflict, gconflict,
			"#%d: conflict = %d, want %d", i, gconflict, tt.wconflict)
	}
}

func TestRLLTIsUpToDate(t *testing.T) {
	previousEnts := []pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2},
		{Index: 3, Term: 3}}
	stable := getTestLogReaderWithoutCache(previousEnts)
	defer removeTestLogdbDir(vfs.GetTestFS())
	defer func() {
		require.NoError(t, stable.logdb.(*ShardedDB).Close())
	}()
	raftLog := raft.NewLog(stable)
	tests := []struct {
		lastIndex uint64
		term      uint64
		wUpToDate bool
	}{
		// greater term, ignore lastIndex
		{raftLog.LastIndex() - 1, 4, true},
		{raftLog.LastIndex(), 4, true},
		{raftLog.LastIndex() + 1, 4, true},
		// smaller term, ignore lastIndex
		{raftLog.LastIndex() - 1, 2, false},
		{raftLog.LastIndex(), 2, false},
		{raftLog.LastIndex() + 1, 2, false},
		// equal term, equal or lager lastIndex wins
		{raftLog.LastIndex() - 1, 3, false},
		{raftLog.LastIndex(), 3, true},
		{raftLog.LastIndex() + 1, 3, true},
	}
	for i, tt := range tests {
		gUpToDate, err := raftLog.UpToDate(tt.lastIndex, tt.term)
		require.NoError(t, err)
		require.Equal(t, tt.wUpToDate, gUpToDate,
			"#%d: uptodate = %v, want %v", i, gUpToDate, tt.wUpToDate)
	}
}

func TestRLLTAppend(t *testing.T) {
	previousEnts := []pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2}}
	tests := []struct {
		ents      []pb.Entry
		windex    uint64
		wents     []pb.Entry
		wunstable uint64
	}{
		{
			[]pb.Entry{},
			2,
			[]pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2}},
			3,
		},
		{
			[]pb.Entry{{Index: 3, Term: 2}},
			3,
			[]pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2},
				{Index: 3, Term: 2}},
			3,
		},
		// conflicts with index 1
		{
			[]pb.Entry{{Index: 1, Term: 2}},
			1,
			[]pb.Entry{{Index: 1, Term: 2}},
			1,
		},
		// conflicts with index 2
		{
			[]pb.Entry{{Index: 2, Term: 3}, {Index: 3, Term: 3}},
			3,
			[]pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 3},
				{Index: 3, Term: 3}},
			2,
		},
	}
	for i, tt := range tests {
		stable := getTestLogReaderWithoutCache(previousEnts)
		raftLog := raft.NewLog(stable)

		fi, _ := stable.GetRange()
		plog.Infof("stable first index: %d", fi)
		require.NoError(t, raftLog.Append(tt.ents))
		index := raftLog.LastIndex()
		require.Equal(t, tt.windex, index,
			"#%d: lastIndex = %d, want %d", i, index, tt.windex)
		g, err := raftLog.Entries(1, math.MaxUint64)
		require.NoError(t, err, "#%d: unexpected error %v", i, err)
		require.Equal(t, tt.wents, g,
			"#%d: logEnts = %+v, want %+v", i, g, tt.wents)
		if goff := raftLog.UnstableOffset(); goff != tt.wunstable {
			t.Errorf("#%d: unstable = %d, want %d", i, goff, tt.wunstable)
		}
		require.NoError(t, stable.logdb.(*ShardedDB).Close())
		removeTestLogdbDir(vfs.GetTestFS())
	}
}

// TestLogMaybeAppend ensures:
// If the given (index, term) matches with the existing log:
//  1. If an existing entry conflicts with a new one (same index
//     but different terms), delete the existing entry and all that
//     follow it
//     2.Append any new entries not already in the log
//
// If the given (index, term) does not match with the existing log:
//
//	return false
func TestRLLTLogMaybeAppend(t *testing.T) {
	previousEnts := []pb.Entry{{Index: 1, Term: 1}, {Index: 2, Term: 2},
		{Index: 3, Term: 3}}
	lastindex := uint64(3)
	lastterm := uint64(3)
	commit := uint64(1)

	tests := []struct {
		logTerm   uint64
		index     uint64
		committed uint64
		ents      []pb.Entry

		wlasti  uint64
		wappend bool
		wcommit uint64
		wpanic  bool
	}{
		// not match: term is different
		{
			lastterm - 1, lastindex, lastindex,
			[]pb.Entry{{Index: lastindex + 1, Term: 4}},
			0, false, commit, false,
		},
		// not match: index out of bound
		{
			lastterm, lastindex + 1, lastindex,
			[]pb.Entry{{Index: lastindex + 2, Term: 4}},
			0, false, commit, false,
		},
		// match with the last existing entry
		{
			lastterm, lastindex, lastindex, nil,
			lastindex, true, lastindex, false,
		},
		{
			lastterm, lastindex, lastindex + 1, nil,
			lastindex, true, lastindex, false, // do not increase commit higher than lastnewi
		},
		{
			lastterm, lastindex, lastindex - 1, nil,
			lastindex, true, lastindex - 1, false, // commit up to the commit in the message
		},
		{
			lastterm, lastindex, 0, nil,
			lastindex, true, commit, false, // commit do not decrease
		},
		{
			0, 0, lastindex, nil,
			0, true, commit, false, // commit do not decrease
		},
		{
			lastterm, lastindex, lastindex,
			[]pb.Entry{{Index: lastindex + 1, Term: 4}},
			lastindex + 1, true, lastindex, false,
		},
		{
			lastterm, lastindex, lastindex + 1,
			[]pb.Entry{{Index: lastindex + 1, Term: 4}},
			lastindex + 1, true, lastindex + 1, false,
		},
		{
			lastterm, lastindex, lastindex + 2,
			[]pb.Entry{{Index: lastindex + 1, Term: 4}},
			lastindex + 1, true, lastindex + 1, false, // do not increase commit higher than lastnewi
		},
		{
			lastterm, lastindex, lastindex + 2,
			[]pb.Entry{{Index: lastindex + 1, Term: 4},
				{Index: lastindex + 2, Term: 4}},
			lastindex + 2, true, lastindex + 2, false,
		},
		// match with the the entry in the middle
		{
			lastterm - 1, lastindex - 1, lastindex,
			[]pb.Entry{{Index: lastindex, Term: 4}},
			lastindex, true, lastindex, false,
		},
		{
			lastterm - 2, lastindex - 2, lastindex,
			[]pb.Entry{{Index: lastindex - 1, Term: 4}},
			lastindex - 1, true, lastindex - 1, false,
		},
		{
			lastterm - 3, lastindex - 3, lastindex,
			[]pb.Entry{{Index: lastindex - 2, Term: 4}},
			lastindex - 2, true, lastindex - 2, true, // conflict with existing committed entry
		},
		{
			lastterm - 2, lastindex - 2, lastindex,
			[]pb.Entry{{Index: lastindex - 1, Term: 4},
				{Index: lastindex, Term: 4}},
			lastindex, true, lastindex, false,
		},
	}

	for i, tt := range tests {
		stable := getTestLogReaderWithoutCache(previousEnts)
		raftLog := raft.NewLog(stable)
		raftLog.SetCommitted(commit)

		if tt.wpanic {
			require.Panics(t, func() {
				glasti, gappend, err := raftLog.TryAppend(tt.index, tt.logTerm,
					tt.committed, tt.ents)
				require.NoError(t, err)
				gcommit := raftLog.GetCommitted()

				require.Equal(t, tt.wlasti, glasti,
					"#%d: lastindex = %d, want %d", i, glasti, tt.wlasti)
				require.Equal(t, tt.wappend, gappend,
					"#%d: append = %v, want %v", i, gappend, tt.wappend)
				require.Equal(t, tt.wcommit, gcommit,
					"#%d: committed = %d, want %d", i, gcommit, tt.wcommit)
				if gappend && len(tt.ents) != 0 {
					gents, err := raftLog.GetEntries(
						raftLog.LastIndex()-uint64(len(tt.ents))+1,
						raftLog.LastIndex()+1, math.MaxUint64)
					require.NoError(t, err)
					require.Equal(t, tt.ents, gents,
						"%d: appended entries = %v, want %v", i, gents, tt.ents)
				}
			})
		} else {
			require.NotPanics(t, func() {
				glasti, gappend, err := raftLog.TryAppend(tt.index, tt.logTerm,
					tt.committed, tt.ents)
				require.NoError(t, err)
				gcommit := raftLog.GetCommitted()

				require.Equal(t, tt.wlasti, glasti,
					"#%d: lastindex = %d, want %d", i, glasti, tt.wlasti)
				require.Equal(t, tt.wappend, gappend,
					"#%d: append = %v, want %v", i, gappend, tt.wappend)
				require.Equal(t, tt.wcommit, gcommit,
					"#%d: committed = %d, want %d", i, gcommit, tt.wcommit)
				if gappend && len(tt.ents) != 0 {
					gents, err := raftLog.GetEntries(
						raftLog.LastIndex()-uint64(len(tt.ents))+1,
						raftLog.LastIndex()+1, math.MaxUint64)
					require.NoError(t, err)
					require.Equal(t, tt.ents, gents,
						"%d: appended entries = %v, want %v", i, gents, tt.ents)
				}
			})
		}

		require.NoError(t, stable.logdb.(*ShardedDB).Close())
		removeTestLogdbDir(vfs.GetTestFS())
	}
}

/*
// TestCompactionSideEffects ensures that all the log related functionality works correctly after
// a compaction.
func TestRLLTCompactionSideEffects(t *testing.T) {
	var i uint64
	// Populate the log with 1000 entries; 750 in stable storage and 250 in unstable.
	lastIndex := uint64(1000)
	unstableIndex := uint64(750)
	lastTerm := lastIndex
	noLimit := uint64(math.MaxUint64)

	entries := make([]pb.Entry, 0)
	for i = 1; i <= unstableIndex; i++ {
		entries = append(entries, pb.Entry{Term: uint64(i), Index: uint64(i)})
	}

	stable := getTestLogReaderWithoutCache(entries)
	raftLog := raft.NewLog(stable)
	for i = unstableIndex; i < lastIndex; i++ {
		raftLog.Append([]pb.Entry{pb.Entry{Term: uint64(i + 1), Index: uint64(i + 1)}})
	}

	ok := raftLog.TryCommit(lastIndex, lastTerm)
	if !ok {
		t.Fatalf("maybeCommit returned false")
	}
	raftLog.AppliedTo(raftLog.GetCommitted())

	offset := uint64(500)
	stable.Compact(offset)

	if raftLog.LastIndex() != lastIndex {
		t.Errorf("lastIndex = %d, want %d", raftLog.LastIndex(), lastIndex)
	}

	for j := offset; j <= raftLog.LastIndex(); j++ {
		if mustTerm(raftLog.Term(j)) != j {
			t.Errorf("term(%d) = %d, want %d", j, mustTerm(raftLog.Term(j)), j)
		}
	}

	for j := offset; j <= raftLog.LastIndex(); j++ {
		if !raftLog.MatchTerm(j, j) {
			t.Errorf("matchTerm(%d) = false, want true", j)
		}
	}

	unstableEnts := raftLog.EntriesToSave()
	if g := len(unstableEnts); g != 250 {
		t.Errorf("len(unstableEntries) = %d, want = %d", g, 250)
	}
	if unstableEnts[0].Index != 751 {
		t.Errorf("Index = %d, want = %d", unstableEnts[0].Index, 751)
	}

	prev := raftLog.LastIndex()
	raftLog.Append([]pb.Entry{pb.Entry{Index: raftLog.LastIndex() + 1, Term: raftLog.LastIndex() + 1}})
	if raftLog.LastIndex() != prev+1 {
		t.Errorf("lastIndex = %d, want = %d", raftLog.LastIndex(), prev+1)
	}

	ents, err := raftLog.Entries(raftLog.LastIndex(), noLimit)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(ents) != 1 {
		t.Errorf("len(entries) = %d, want = %d", len(ents), 1)
	}

	stable.logdb.(*ShardedDB).Close()
	removeTestLogdbDir()
}
*/

func TestRLLTHasNextEnts(t *testing.T) {
	snap := pb.Snapshot{
		Term: 1, Index: 3,
	}
	ents := []pb.Entry{
		{Term: 1, Index: 4},
		{Term: 1, Index: 5},
		{Term: 1, Index: 6},
	}
	tests := []struct {
		applied uint64
		hasNext bool
	}{
		{0, true},
		{3, true},
		{4, true},
		{5, false},
	}
	for i, tt := range tests {
		stable := getTestLogReaderWithoutCache(nil)
		require.NoError(t, stable.ApplySnapshot(snap))
		raftLog := raft.NewLog(stable)
		require.NoError(t, raftLog.Append(ents))
		_, err := raftLog.TryCommit(5, 1)
		require.NoError(t, err)
		raftLog.AppliedTo(tt.applied)

		hasNext := raftLog.HasEntriesToApply()
		require.Equal(t, tt.hasNext, hasNext,
			"#%d: hasNext = %v, want %v", i, hasNext, tt.hasNext)

		require.NoError(t, stable.logdb.(*ShardedDB).Close())
		removeTestLogdbDir(vfs.GetTestFS())
	}
}

func TestRLLTNextEnts(t *testing.T) {
	snap := pb.Snapshot{
		Term: 1, Index: 3,
	}
	ents := []pb.Entry{
		{Term: 1, Index: 4},
		{Term: 1, Index: 5},
		{Term: 1, Index: 6},
	}
	tests := []struct {
		applied uint64
		wents   []pb.Entry
	}{
		{0, ents[:2]},
		{3, ents[:2]},
		{4, ents[1:2]},
		{5, nil},
	}
	for i, tt := range tests {
		stable := getTestLogReaderWithoutCache(nil)
		require.NoError(t, stable.ApplySnapshot(snap))
		raftLog := raft.NewLog(stable)
		require.NoError(t, raftLog.Append(ents))
		_, err := raftLog.TryCommit(5, 1)
		require.NoError(t, err)
		raftLog.AppliedTo(tt.applied)

		nents, err := raftLog.EntriesToApply()
		require.NoError(t, err)
		require.Equal(t, tt.wents, nents,
			"#%d: nents = %+v, want %+v", i, nents, tt.wents)

		require.NoError(t, stable.logdb.(*ShardedDB).Close())
		removeTestLogdbDir(vfs.GetTestFS())
	}
}

// TestCompaction ensures that the number of log entries is correct after compactions.
func TestRLLTCompaction(t *testing.T) {
	tests := []struct {
		lastIndex uint64
		compact   []uint64
		wleft     []int
		wallow    bool
	}{
		// out of upper bound
		{1000, []uint64{1001}, []int{-1}, false},
		{1000, []uint64{300, 500, 800, 900}, []int{700, 500, 200, 100}, true},
		// out of lower bound
		{1000, []uint64{300, 299}, []int{700, -1}, false},
	}

	for i, tt := range tests {
		entries := make([]pb.Entry, 0)
		for i := uint64(1); i <= tt.lastIndex; i++ {
			entries = append(entries, pb.Entry{Index: i})
		}
		stable := getTestLogReaderWithoutCache(entries)
		raftLog := raft.NewLog(stable)

		func() {
			defer func() {
				if r := recover(); r != nil {
					if tt.wallow {
						t.Errorf("%d: allow = %v, want %v: %v", i, false, true, r)
					}
				}
			}()

			if _, err := raftLog.TryCommit(tt.lastIndex, 0); err != nil {
				t.Fatalf("unexpected error %v", err)
			}
			raftLog.AppliedTo(raftLog.GetCommitted())

			for j := 0; j < len(tt.compact); j++ {
				err := stable.Compact(tt.compact[j])
				if err != nil {
					if tt.wallow {
						t.Errorf("#%d.%d allow = %t, want %t", i, j, false, tt.wallow)
					}
					continue
				}
				if len(raftLog.AllEntries()) != tt.wleft[j] {
					t.Errorf("#%d.%d len = %d, want %d", i, j, len(raftLog.AllEntries()), tt.wleft[j])
				}
			}
		}()
		require.NoError(t, stable.logdb.(*ShardedDB).Close())
		removeTestLogdbDir(vfs.GetTestFS())
	}
}

func TestRLLTLogRestore(t *testing.T) {
	index := uint64(1000)
	term := uint64(1000)
	stable := getTestLogReaderWithoutCache(nil)
	require.NoError(t, stable.ApplySnapshot(pb.Snapshot{Index: index, Term: term}))
	raftLog := raft.NewLog(stable)

	require.Equal(t, 0, len(raftLog.AllEntries()),
		"len = %d, want 0", len(raftLog.AllEntries()))
	require.Equal(t, index+1, raftLog.FirstIndex(),
		"firstIndex = %d, want %d", raftLog.FirstIndex(), index+1)
	require.Equal(t, index, raftLog.GetCommitted(),
		"committed = %d, want %d", raftLog.GetCommitted(), index)
	require.Equal(t, index+1, raftLog.UnstableOffset(),
		"unstable = %d, want %d", raftLog.UnstableOffset(), index+1)
	require.Equal(t, term, mustTerm(raftLog.Term(index)),
		"term = %d, want %d", mustTerm(raftLog.Term(index)), term)

	require.NoError(t, stable.logdb.(*ShardedDB).Close())
	removeTestLogdbDir(vfs.GetTestFS())
}

func TestRLLTIsOutOfBounds(t *testing.T) {
	offset := uint64(100)
	num := uint64(100)

	stable := getTestLogReaderWithoutCache(nil)
	require.NoError(t, stable.ApplySnapshot(pb.Snapshot{Index: offset}))
	l := raft.NewLog(stable)

	for i := uint64(1); i <= num; i++ {
		require.NoError(t, l.Append([]pb.Entry{{Index: i + offset}}))
	}

	first := offset + 1
	tests := []struct {
		lo, hi        uint64
		wpanic        bool
		wErrCompacted bool
	}{
		{
			first - 2, first + 1,
			false,
			true,
		},
		{
			first - 1, first + 1,
			false,
			true,
		},
		{
			first, first,
			false,
			false,
		},
		{
			first + num/2, first + num/2,
			false,
			false,
		},
		{
			first + num - 1, first + num - 1,
			false,
			false,
		},
		{
			first + num, first + num,
			false,
			false,
		},
		{
			first + num, first + num + 1,
			true,
			false,
		},
		{
			first + num + 1, first + num + 1,
			true,
			false,
		},
	}

	for i, tt := range tests {
		if tt.wpanic {
			require.Panics(t, func() {
				err := l.CheckBound(tt.lo, tt.hi)
				require.NoError(t, err)
			})
		} else {
			require.NotPanics(t, func() {
				err := l.CheckBound(tt.lo, tt.hi)
				if tt.wErrCompacted {
					require.Equal(t, raft.ErrCompacted, err,
						"%d: err = %v, want %v", i, err, raft.ErrCompacted)
				} else {
					require.NoError(t, err, "%d: unexpected err %v", i, err)
				}
			})
		}
	}

	require.NoError(t, stable.logdb.(*ShardedDB).Close())
	removeTestLogdbDir(vfs.GetTestFS())
}

func TestRLLTTerm(t *testing.T) {
	var i uint64
	offset := uint64(100)
	num := uint64(100)

	stable := getTestLogReaderWithoutCache(nil)
	require.NoError(t, stable.ApplySnapshot(pb.Snapshot{Index: offset, Term: 1}))
	l := raft.NewLog(stable)
	for i = 1; i < num; i++ {
		require.NoError(t, l.Append([]pb.Entry{{Index: offset + i, Term: i}}))
	}

	tests := []struct {
		index uint64
		w     uint64
	}{
		{offset - 1, 0},
		{offset, 1},
		{offset + num/2, num / 2},
		{offset + num - 1, num - 1},
		{offset + num, 0},
	}

	for j, tt := range tests {
		term := mustTerm(l.Term(tt.index))
		require.Equal(t, tt.w, term, "#%d: at = %d, want %d", j, term, tt.w)
	}

	require.NoError(t, stable.logdb.(*ShardedDB).Close())
	removeTestLogdbDir(vfs.GetTestFS())
}

// not related to logstable
/*
func TestTermWithUnstableSnapshot(t *testing.T) {
}*/

func TestRLLTSlice(t *testing.T) {
	var i uint64
	offset := uint64(100)
	num := uint64(100)
	last := offset + num
	half := offset + num/2
	halfe := pb.Entry{Index: half, Term: half}
	noLimit := uint64(math.MaxUint64)

	stable := getTestLogReaderWithoutCache(nil)
	require.NoError(t, stable.ApplySnapshot(pb.Snapshot{Index: offset}))
	for i = 1; i < num/2; i++ {
		require.NoError(t, stable.Append([]pb.Entry{{Index: offset + i, Term: offset + i}}))
		ud := pb.Update{
			ShardID:       LogReaderTestShardID,
			ReplicaID:     LogReaderTestReplicaID,
			EntriesToSave: []pb.Entry{{Index: offset + i, Term: offset + i}},
		}
		require.NoError(t, stable.logdb.SaveRaftState([]pb.Update{ud}, 1))
	}
	l := raft.NewLog(stable)
	for i = num / 2; i < num; i++ {
		require.NoError(t, l.Append([]pb.Entry{{Index: offset + i, Term: offset + i}}))
	}

	tests := []struct {
		from  uint64
		to    uint64
		limit uint64

		w      []pb.Entry
		wpanic bool
	}{
		// test no limit
		{offset - 1, offset + 1, noLimit, nil, false},
		{offset, offset + 1, noLimit, nil, false},
		{half - 1, half + 1, noLimit,
			[]pb.Entry{{Index: half - 1, Term: half - 1}, {Index: half, Term: half}}, false},
		{half, half + 1, noLimit, []pb.Entry{{Index: half, Term: half}}, false},
		{last - 1, last, noLimit, []pb.Entry{{Index: last - 1, Term: last - 1}}, false},
		{last, last + 1, noLimit, nil, true},

		// test limit
		{half - 1, half + 1, 0,
			[]pb.Entry{{Index: half - 1, Term: half - 1}}, false},
		{half - 1, half + 1, uint64(halfe.SizeUpperLimit() + 1),
			[]pb.Entry{{Index: half - 1, Term: half - 1}}, false},
		{half - 2, half + 1, uint64(halfe.SizeUpperLimit() + 1),
			[]pb.Entry{{Index: half - 2, Term: half - 2}}, false},
		{half - 1, half + 1, uint64(halfe.SizeUpperLimit() * 2),
			[]pb.Entry{{Index: half - 1, Term: half - 1}, {Index: half, Term: half}}, false},
		{half - 1, half + 2, uint64(halfe.SizeUpperLimit() * 3),
			[]pb.Entry{{Index: half - 1, Term: half - 1}, {Index: half, Term: half},
				{Index: half + 1, Term: half + 1}}, false},
		{half, half + 2, uint64(halfe.SizeUpperLimit()),
			[]pb.Entry{{Index: half, Term: half}}, false},
		{half, half + 2, uint64(halfe.SizeUpperLimit() * 2),
			[]pb.Entry{{Index: half, Term: half}, {Index: half + 1, Term: half + 1}}, false},
	}

	for j, tt := range tests {
		if tt.wpanic {
			require.Panics(t, func() {
				g, err := l.GetEntries(tt.from, tt.to, tt.limit)
				require.NoError(t, err)
				require.Equal(t, tt.w, g,
					"#%d: from %d to %d = %v, want %v", j, tt.from, tt.to, g, tt.w)
			})
		} else {
			require.NotPanics(t, func() {
				g, err := l.GetEntries(tt.from, tt.to, tt.limit)
				if tt.from <= offset {
					require.Equal(t, raft.ErrCompacted, err,
						"#%d: err = %v, want %v", j, err, raft.ErrCompacted)
				} else {
					require.NoError(t, err, "#%d: unexpected error %v", j, err)
				}
				require.Equal(t, tt.w, g,
					"#%d: from %d to %d = %v, want %v", j, tt.from, tt.to, g, tt.w)
			})
		}
	}

	require.NoError(t, stable.logdb.(*ShardedDB).Close())
	removeTestLogdbDir(vfs.GetTestFS())
}

func mustTerm(term uint64, err error) uint64 {
	if err != nil {
		panic(err)
	}
	return term
}
