package teams

import (
	"fmt"
	"log"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	context "golang.org/x/net/context"

	"github.com/keybase/client/go/engine"
	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
	"github.com/ugorji/go/codec"
)

// TODO bust in-memory and disk cache when roles change!

// Store TeamData's on memory and disk. Threadsafe.
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

func (s *Storage) Put(ctx context.Context, state keybase1.TeamData) {
	s.Lock()
	defer s.Unlock()

	s.mem.Put(ctx, state)

	err := s.disk.Put(ctx, state)
	if err != nil {
		s.G().Log.Warningf("teams.Storage.Put err: %v", err)
	}
}

// Can return nil.
func (s *Storage) Get(ctx context.Context, teamID keybase1.TeamID) *keybase1.TeamData {
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

// Store TeamData's on disk. Threadsafe.
type DiskStorage struct {
	libkb.Contextified
	sync.Mutex
}

// Increment to invalidate the disk cache.
const DiskStorageVersion = 1

type DiskStorageItem struct {
	Version int               `codec:"V"`
	State   keybase1.TeamData `codec:"S"`
}

func NewDiskStorage(g *libkb.GlobalContext) *DiskStorage {
	return &DiskStorage{
		Contextified: libkb.NewContextified(g),
	}
}

func (s *DiskStorage) Put(ctx context.Context, state keybase1.TeamData) error {
	s.Lock()
	defer s.Unlock()

	key := s.dbKey(ctx, state.Chain.Id)
	item := DiskStorageItem{
		Version: DiskStorageVersion,
		State:   state,
	}

	clearData, err := s.encode(ctx, item)
	if err != nil {
		return err
	}

	// TODO encrypt

	s.G().LocalChatDb.PutRaw(key, bs)
	if err != nil {
		return err
	}
	return nil
}

// Res is valid if (found && err == nil)
func (s *DiskStorage) Get(ctx context.Context, teamID keybase1.TeamID) (res keybase1.TeamData, found bool, err error) {
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

	// Sanity check
	// TODO might as well check reader
	if len(item.State.Chain.Id) == 0 {
		return res, false, fmt.Errorf("decode from disk had empty team id")
	}
	if !item.State.Chain.Id.Eq(teamID) {
		return res, false, fmt.Errorf("decode from disk had wrong team id %v != %v", item.State.Chain.Id, teamID)
	}

	return item.State, true, nil
}

func (s *DiskStorage) dbKey(ctx context.Context, teamID keybase1.TeamID) libkb.DbKey {
	return libkb.DbKey{
		Typ: libkb.DBChatInbox,
		// TODO make sure subteam don't clobber each other
		// TODO is this the right thing to key by?
		Key: fmt.Sprintf("tid:%s", teamID),
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

func (s *MemoryStorage) Put(ctx context.Context, state keybase1.TeamData) {
	s.lru.Add(state.Chain.Id, state)
}

// Can return nil.
func (s *MemoryStorage) Get(ctx context.Context, teamID keybase1.TeamID) *keybase1.TeamData {
	untyped, ok := s.lru.Get(teamID)
	if !ok {
		return nil
	}
	state, ok := untyped.(keybase1.TeamData)
	if !ok {
		s.G().Log.Warning("Team MemoryStorage got bad type from lru")
		return nil
	}
	return &state
}

// --------------------------------------------------

// ***
// If we change this, make sure to update libkb.EncryptionReasonTeamsLocalStorage as well!
// To avoid using the same derived with two crypto algorithms.
// ***
const localStorageCryptoVersion = 1

func getLocalStorageSecretBoxKey(ctx context.Context, g *libkb.GlobalContext, getSecretUI func() libkb.SecretUI) (fkey [32]byte, err error) {
	// Get secret device key
	encKey, err := engine.GetMySecretKey(ctx, g, getSecretUI, libkb.DeviceEncryptionKeyType,
		"encrypt teams storage")
	if err != nil {
		return fkey, err
	}
	kp, ok := encKey.(libkb.NaclDHKeyPair)
	if !ok || kp.Private == nil {
		return fkey, libkb.KeyCannotDecryptError{}
	}

	// Derive symmetric key from device key
	skey, err := encKey.SecretSymmetricKey(libkb.EncryptionReasonTeamsLocalStorage)
	if err != nil {
		return fkey, err
	}

	copy(fkey[:], skey[:])
	return fkey, nil
}
