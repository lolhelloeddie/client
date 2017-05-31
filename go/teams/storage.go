package teams

import (
	"fmt"
	"log"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	context "golang.org/x/net/context"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/ugorji/go/codec"
)

// Store TeamSigChainState's on memory and disk. Threadsafe.
type Storage struct {
	libkb.Contextified
	sync.Mutex
	mem  *MemoryStorage
	disk *DiskStorage
}

func NewStorage(g *libkb.GlobalContext) *Storage {
	return &Storage{
		Contextified: libkb.NewContextified(g),
		mem:          NewMemoryStorage(g),
		disk:         NewDiskStorage(g),
	}
}

func (s *Storage) Put(ctx context.Context, state TeamSigChainState) {
	s.Lock()
	defer s.Unlock()

	s.mem.Put(ctx, state.GetID(), state)

	err := s.disk.Put(ctx, state)
	if err != nil {
		s.G().Log.Warningf("teams.Storage.Put err: %v", err)
	}
}

// Can return nil.
func (s *Storage) Get(ctx context.Context, teamID keybase1.TeamID) *TeamSigChainState {
	s.Lock()
	defer s.Unlock()

	item := s.mem.Get(ctx, teamID)
	if item != nil {
		// Mem hit
		return item
	}

	res, found, err := s.disk.Get(ctx, teamID)
	if found && err == nil {
		// Disk hit
		return res
	}

	return nil
}

// --------------------------------------------------

// Store TeamSigChainState's on disk. Threadsafe.
type DiskStorage struct {
	libkb.Contextified
	sync.Mutex
}

// Increment to invalidate the disk cache.
const DiskStorageVersion = 1

type DiskStorageItem struct {
	Version int                        `codec:"V"`
	State   keybase1.TeamSigChainState `codec:"S"`
}

func NewDiskStorage(g *libkb.GlobalContext) *DiskStorage {
	return &DiskStorage{
		Contextified: libkb.NewContextified(g),
	}
}

func (s *DiskStorage) Put(ctx context.Context, state TeamSigChainState) error {
	s.Lock()
	defer s.Unlock()

	key := s.dbKey(ctx, state.GetID())
	item := DiskStorageItem{
		Version: DiskStorageVersion,
		State:   state.inner,
	}

	bs, err := s.encode(ctx, item)
	if err != nil {
		return err
	}

	s.G().LocalChatDb.PutRaw(key, bs)
	if err != nil {
		return err
	}
	return nil
}

// Res is valid if (found && err == nil)
func (s *DiskStorage) Get(ctx context.Context, teamID keybase1.TeamID) (res TeamSigChainState, found bool, err error) {
	s.Lock()
	defer s.Unlock()

	key := s.dbKey(ctx, teamID)
	bs, found, err := s.G().LocalDb.GetRaw(key)
	if err != nil {
		return res, false, err
	}
	if !found {
		return res, false, nil
	}
	var item DiskStorageItem
	err = s.decode(ctx, bs, &item)
	if err != nil {
		return res, true, err
	}
	return TeamSigChainState{
		inner: item.State,
	}, true, nil
}

func (s *DiskStorage) dbKey(ctx context.Context, teamID keybase1.TeamID) libkb.DbKey {
	return libkb.DbKey{
		Typ: libkb.DBChatInbox,
		// TODO make sure subteam don't clobber each other
		// TODO is this the right thing to key by?
		Key: fmt.Sprintf("ts:%s", teamID),
	}
}

func (s *DiskStorage) encode(ctx context.Context, input interface{}) ([]byte, error) {
	mh := codec.MsgpackHandle{WriteExt: true}
	var data []byte
	enc := codec.NewEncoderBytes(&data, &mh)
	if err := enc.Encode(input); err != nil {
		return nil, err
	}
	return data, nil
}

func (s *DiskStorage) decode(ctx context.Context, data []byte, res interface{}) error {
	mh := codec.MsgpackHandle{WriteExt: true}
	dec := codec.NewDecoderBytes(data, &mh)
	err := dec.Decode(res)
	return err
}

// --------------------------------------------------

const MEM_CACHE_LRU_SIZE = 50

// Store some TeamSigChainState's in memory. Threadsafe.
type MemoryStorage struct {
	libkb.Contextified
	lru *lru.Cache
}

func NewMemoryStorage(g *libkb.GlobalContext) *MemoryStorage {
	nlru, err := lru.New(MEM_CACHE_LRU_SIZE)
	if err != nil {
		// lru.New only panics if size <= 0
		log.Panicf("Could not create lru cache: %v", err)
	}
	return &MemoryStorage{
		Contextified: libkb.NewContextified(g),
		lru:          nlru,
	}
}

func (s *MemoryStorage) Put(ctx context.Context, state TeamSigChainState) {
	s.lru.Add(state.GetID(), state)
}

// Can return nil.
func (s *MemoryStorage) Get(ctx context.Context, teamID keybase1.TeamID) *TeamSigChainState {
	untyped, ok := s.lru.Get(teamID)
	if !ok {
		return nil
	}
	state, ok := untyped.(TeamSigChainState)
	if !ok {
		s.G().Log.Warning("Team MemoryStorage got bad type from lru")
		return nil
	}
	return &state
}
