package teams

import (
	"encoding/json"

	"golang.org/x/net/context"

	"github.com/keybase/client/go/libkb"
	"github.com/keybase/client/go/protocol/keybase1"
)

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

	ForceReload bool
	StaleOK     bool // if stale cached versions are OK (for immutable fields)
	NoNetwork   bool // make no network requests
}

func (l *TeamLoader) Load(ctx context.Context, lArg LoadTeamArg) (Something, error) {
	type infoT struct {
		hitCache         bool
		loadedFromServer bool
	}
	var info infoT

	if lArg.ForceReload {
		panic("TODO")
	}

	state := l.storage.Get(lArg)
	info.hitCache == (state != nil)
	if state == nil {
		panic("TODO")
	}

	if info.loadedFromServer {
		l.storage.Put(ctx, qq, qq)
	}

	return err
}

func (l *TeamLoader) loadFromServer(ctx context.Context, lArg LoadTeamArg) error {
	// TODO check load arg for id|name

	sArg := libkb.NewRetryAPIArg("team/get")
	sArg.NetContext = ctx
	sArg.SessionType = libkb.APISessionTypeREQUIRED
	sArg.Args = libkb.HTTPArgs{
		"name": libkb.S{Val: string(lArg.Name)},
		"id":   libkb.S{Val: lArg.ID.String()},
		// TODO used cached last seqno 0
		"low": libkb.I{Val: 0},
	}
	var rt rawTeam
	if err := l.G().API.GetDecode(sArg, &rt); err != nil {
		return err
	}
	return nil
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
