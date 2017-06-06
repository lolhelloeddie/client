package teams

import (
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
)

// Threadsafe.
type TeamLoader struct {
	libkb.Contextified
	storage *Storage
}

func NewTeamLoader(g *libkb.GlobalContext, storage *Storage) *TeamLoader {
	return &TeamLoader{
		Contextified: libkb.NewContextified(g),
		storage:      storage,
	}
}

// TODO change this to return a friendly version of TeamData. Perhaps Team.
func (l *TeamLoader) Load(ctx context.Context, lArg libkb.LoadTeamArg) (res *keybase1.TeamData, err error) {
	// GIANT TODO: check for role change
	// GIANT TODO: load recursively to load subteams

	err = lArg.Check()
	if err != nil {
		return nil, err
	}

	type infoT struct {
		hitCache         bool
		loadedFromServer bool
	}
	var info infoT
	defer l.G().Log.CDebugf(ctx, "TeamLoader#Load info:%+v", info)

	teamID := lArg.ID
	if len(lArg.ID) == 0 {
		teamName, err := TeamNameFromString(lArg.Name)
		if err != nil {
			return nil, err
		}
		if teamName.IsSubTeam() {
			return nil, fmt.Errorf("TODO: support loading subteams by name")
		}
		teamID = teamName.ToTeamID()
	}

	if lArg.ForceFullReload {
		if lArg.NoNetwork {
			return nil, fmt.Errorf("cannot force full reload with no-network set")
		}
		res, err = l.loadFromServerFromScratch(ctx, lArg)
		if err != nil {
			return nil, err
		}
		info.loadedFromServer = true
	}

	if res == nil {
		// Load from cache
		state := l.storage.Get(ctx, teamID)
		info.hitCache = (state != nil)
		if state == nil {
			panic("TODO")
		}
	}

	if lArg.ForceSync && !lArg.ForceFullReload {
		if lArg.NoNetwork {
			return nil, fmt.Errorf("cannot force sync with no-network set")
		}
		panic("TODO")
	}

	// TODO check freshness and load increment from server

	if info.loadedFromServer {
		if res == nil {
			return nil, fmt.Errorf("team loader fault: loaded from server but no result")
		}
		l.storage.Put(ctx, *res)
	}

	if res == nil {
		return nil, fmt.Errorf("team loader fault: no result but no error")
	}
	return res, nil
}

// Load a team from the server with no cached data.
func (l *TeamLoader) loadFromServerFromScratch(ctx context.Context, lArg libkb.LoadTeamArg) (*keybase1.TeamData, error) {
	sArg := libkb.NewRetryAPIArg("team/get")
	sArg.NetContext = ctx
	sArg.SessionType = libkb.APISessionTypeREQUIRED
	sArg.Args = libkb.HTTPArgs{
		// "name": libkb.S{Val: string(lArg.Name)},
		"id": libkb.S{Val: lArg.ID.String()},
		// TODO used cached last seqno 0 (in a non from-scratch function)
		"low": libkb.I{Val: 0},
	}
	var rt rawTeam
	if err := l.G().API.GetDecode(sArg, &rt); err != nil {
		return nil, err
	}

	links, err := l.parseChainLinks(ctx, &rt)
	if err != nil {
		return nil, err
	}

	player, err := l.newPlayer(ctx, links)
	if err != nil {
		return nil, err
	}

	state, err := player.GetState()
	if err != nil {
		return nil, err
	}

	// TODO (non-critical) validate reader key masks

	res := keybase1.TeamData{
		Chain:           state.inner,
		PerTeamKeySeeds: nil,
		ReaderKeyMasks:  rt.ReaderKeyMasks,
	}

	seed, err := l.openBox(ctx, rt.Box, state)
	if err != nil {
		return nil, err
	}
	res.PerTeamKeySeeds = append(res.PerTeamKeySeeds, *seed)

	// TODO receive prevs (and sort seeds list)

	return &res, nil
}

func (l *TeamLoader) parseChainLinks(ctx context.Context, rawTeam *rawTeam) ([]SCChainLink, error) {
	var links []SCChainLink
	for _, raw := range rawTeam.Chain {
		link, err := ParseTeamChainLink(string(raw))
		if err != nil {
			return nil, err
		}
		links = append(links, link)
	}
	return links, nil
}

func (l *TeamLoader) newPlayer(ctx context.Context, links []SCChainLink) (*TeamSigChainPlayer, error) {
	// TODO determine whether this player is for a subteam
	isSubteam := false
	// TODO get our real eldest seqno.
	me := NewUserVersion(l.G().Env.GetUsername().String(), 1)

	f := newFinder(l.G())
	player := NewTeamSigChainPlayer(l.G(), f, me, isSubteam)
	if err := player.AddChainLinks(ctx, links); err != nil {
		return nil, err
	}
	return player, nil
}

func (l *TeamLoader) openBox(ctx context.Context, box TeamBox, chain TeamSigChainState) (*keybase1.PerTeamKeySeedItem, error) {
	userEncKey, err := l.perUserEncryptionKeyForBox(ctx, box)
	if err != nil {
		return nil, err
	}

	secret, err := box.Open(userEncKey)
	if err != nil {
		return nil, err
	}

	keyManager, err := NewTeamKeyManagerWithSecret(l.G(), secret, box.Generation)
	if err != nil {
		return nil, err
	}

	signingKey, err := keyManager.SigningKey()
	if err != nil {
		return nil, err
	}
	encryptionKey, err := keyManager.EncryptionKey()
	if err != nil {
		return nil, err
	}

	teamKey, err := chain.GetPerTeamKeyAtGeneration(int(box.Generation))
	if err != nil {
		return nil, err
	}

	if !teamKey.SigKID.SecureEqual(signingKey.GetKID()) {
		return nil, errors.New("derived signing key did not match key in team chain")
	}

	if !teamKey.EncKID.SecureEqual(encryptionKey.GetKID()) {
		return nil, errors.New("derived encryption key did not match key in team chain")
	}

	// TODO: check that t.Box.SenderKID is a known device DH key for the
	// user that signed the link.
	// See CORE-5399

	seed, err := libkb.MakeByte32Soft(secret)
	if err != nil {
		return nil, fmt.Errorf("invalid seed: %v", err)
	}

	record := keybase1.PerTeamKeySeedItem{
		Seed:       seed,
		Generation: int(box.Generation),
		Seqno:      teamKey.Seqno,
	}

	return &record, nil
}

func (t *TeamLoader) perUserEncryptionKeyForBox(ctx context.Context, box TeamBox) (*libkb.NaclDHKeyPair, error) {
	kr, err := t.G().GetPerUserKeyring()
	if err != nil {
		return nil, err
	}
	// XXX this seems to be necessary:
	if err := kr.Sync(ctx); err != nil {
		return nil, err
	}
	encKey, err := kr.GetEncryptionKeyBySeqno(ctx, box.PerUserKeySeqno)
	if err != nil {
		return nil, err
	}

	return encKey, nil
}

// Response from server
type rawTeam struct {
	Status         libkb.AppStatus
	Chain          []json.RawMessage
	Box            TeamBox
	ReaderKeyMasks []keybase1.ReaderKeyMask `json:"reader_key_masks"`
}

func (r *rawTeam) GetAppStatus() *libkb.AppStatus {
	return &r.Status
}
