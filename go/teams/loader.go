package teams

import (
	"encoding/json"
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

type LoadTeamArg struct {
	// One of these must be specified.
	// If both are specified ID will be used and Name will be checked.
	ID   keybase1.TeamID
	Name TeamName

	ForceFullReload bool // ignore local data and fetch from the server
	ForceSync       bool // require a fresh sync with the server
	StaleOK         bool // if stale cached versions are OK
	NoNetwork       bool // make no network requests
}

func (a *LoadTeamArg) check() error {
	hasID := len(a.ID) > 0
	hasName := len(a.Name) > 0
	if hasID && hasName {
		return fmt.Errorf("team load arg has both ID and Name")
	}
	if !hasID && !hasName {
		return fmt.Errorf("team load arg must have one of ID and Name")
	}
	return nil
}

// TODO change this to return a frienldy version of TeamData. Perhaps Team.
func (l *TeamLoader) Load(ctx context.Context, lArg LoadTeamArg) (*keybase1.TeamData, error) {
	err := lArg.check()
	if err != nil {
		return nil, err
	}

	type infoT struct {
		hitCache         bool
		loadedFromServer bool
	}
	var info infoT

	if lArg.ForceFullReload {
		panic("TODO")
	}

	if lArg.ForceSync {
		panic("TODO")
	}

	if len(lArg.ID) == 0 {
		panic("TODO support load by team name")
	}
	teamID := lArg.ID

	state := l.storage.Get(ctx, teamID)
	info.hitCache == (state != nil)
	if state == nil {
		panic("TODO")
	}

	if info.loadedFromServer {
		panic("TODO store")
		// l.storage.Put(ctx, qq, qq)
	}

	return state, err
}

func (l *TeamLoader) loadFromServerFromScratch(ctx context.Context, lArg LoadTeamArg) (*keybase1.TeamData, error) {
	sArg := libkb.NewRetryAPIArg("team/get")
	sArg.NetContext = ctx
	sArg.SessionType = libkb.APISessionTypeREQUIRED
	sArg.Args = libkb.HTTPArgs{
		"name": libkb.S{Val: string(lArg.Name)},
		"id":   libkb.S{Val: lArg.ID.String()},
		// TODO used cached last seqno 0 (in a non from-scratch function)
		"low": libkb.I{Val: 0},
	}
	var rt rawTeam
	if err := l.G().API.GetDecode(sArg, &rt); err != nil {
		return nil, err
	}

	links, err := l.parseChainLinks(ctx, rt)
	if err != nil {
		return err
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

	rt.Box

	keybase1.TeamData{
		Chain:           state,
		PerTeamKeySeeds: TODO,
		ReaderKeyMasks:  rt.ReaderKeyMasks,
	}

	return nil
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
	// TODO get our real eldest seqno.
	// TODO determine whether really subteam
	isSubteam := false
	player := NewTeamSigChainPlayer(f.G(), f, NewUserVersion(f.G().Env.GetUsername().String(), 1), isSubteam)
	if err := player.AddChainLinks(ctx, links); err != nil {
		return nil, err
	}
	return player, nil
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
