package app

import (
	"encoding/json"
	"fmt"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/viewmodel"
)

func (a *app) accountServices() viewmodel.AccountServices {
	store, restore, identityOf := a.identityCache, restoreLoginSession, loginIdentity
	return viewmodel.AccountServices{
		Offline: func() bool { return a.network.offline }, Stopped: func() bool { return a.quitting },
		Post: a.post, Ask: accountAsk, Identity: identityOf, Save: saveLoginSession, Clear: clearLoginSession, Cookies: netease.SessionCookies, Logout: logoutSession,
		Restore: func() (*netease.Profile, bool) {
			restored := restore()
			var kept *netease.Profile
			identity := identityOf()
			if restored && identity != "" {
				b, _ := store.Read(identity, "profile")
				if json.Unmarshal(b, &kept) != nil || kept != nil && kept.UserID <= 0 {
					kept = nil
				}
			}
			return kept, restored
		},
		NewQR: func() (string, viewmodel.QRLoginAttempt, error) {
			attempt := netease.NewQRLogin()
			key, err := attempt.Key()
			return key, attempt, err
		},
		Change: a.signedIn, Discard: a.discardAccount, ClearIdentity: func() { a.identityCache.Clear("", nil) }, StopReports: func() { a.stopPlayReports() }, PickUp: a.pickUp, Notify: func(message string) { a.toast = message },
		Verified: func(prof *netease.Profile, identity string) {
			a.bindOfflineLyrics()
			if identity != "" {
				a.identityCache.Queue(identity, "profile", cacheBytes(prof), a.identityCache.Epoch())
			}
			a.freshenLibrary()
		},
	}
}
func (a *app) boot()           { a.home.Loading = true; a.account.Boot(a.accountServices()) }
func (a *app) confirmAccount() { a.account.Confirm(a.accountServices()) }
func (a *app) openLogin()      { a.account.OpenLogin(a.accountServices()) }
func (a *app) checkLogin(gen int, key string, attempt viewmodel.QRLoginAttempt) bool {
	return a.account.CheckLogin(gen, key, attempt, a.accountServices())
}
func (a *app) signOut() { a.account.SignOut(a.accountServices()) }

// Session adapters preserve the existing cache, player and page lifecycle.
func (a *app) discardAccount() {
	if a.account.Profile != nil {
		a.browseCache.Clear(fmt.Sprint(a.account.Profile.UserID), nil)
	}
}

func (a *app) signedIn(prof *netease.Profile) {
	a.cancelDownloads()
	a.stopPlayReports()
	if a.recovery.timer != nil {
		a.recovery.timer.Stop()
		a.recovery.timer = nil
	}
	a.account.Accept(prof)
	a.library.Reset()
	a.bindOfflineLyrics()
	if identity := loginIdentity(); identity != "" && prof != nil {
		a.identityCache.Queue(identity, "profile", cacheBytes(prof), a.identityCache.Epoch())
	}
	a.home.Reset()
	// Playback picks up where it was left, once the session that lets
	// the listener play their songs is back.
	a.restoreSession()

	// What another listener played is not what this one played.
	a.recent.Reset()
	a.cancelCloudLoad()
	a.cloud = viewmodel.Cloud{}

	// What the listener may play, and follows, changed: load the pages
	// again.

	a.artists = newPageCache[int64, *artistData](artistBusy)
	a.albums = newPageCache[int64, *albumData](albumBusy)
	a.users = newPageCache[int64, *userData](userBusy)
	a.podcasts = newPageCache[int64, *podcastData](podcastBusy)
	a.podcastTab = &podcastTab{}
	a.searches = newPageCache[route, *searchData](searchBusy)
	a.load(a.route)
	a.loadHome()
	a.openPendingMusicLink()
	if prof == nil {
		return
	}
	a.loadLiked(prof.UserID)
	a.loadMine(prof.UserID)
	a.loadSubscribedAlbums(prof.UserID)
}

var accountAsk = netease.Account
var restoreLoginSession = netease.RestoreSession
var saveLoginSession = netease.SaveSession
var clearLoginSession = netease.ClearSession
var logoutSession = netease.Logout
var loginIdentity = netease.SessionIdentity
