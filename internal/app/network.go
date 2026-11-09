package app

import (
	"github.com/egoist/mygo"
	"netease-music-mygo/internal/netease"
)

type networkState struct {
	known, offline bool
	recovering     bool
	stop           func()
}

var readNetworkStatus = mygo.Network.Status

// System notifications also detect disconnects on an idle, fully cached page.
// They report a network route, not that NetEase answers: request failures keep
// their existing recovery/backoff rather than being classified as a logout.
func (a *app) watchNetwork() {
	a.network.known = true
	a.network.offline = !readNetworkStatus().Online
	netease.SetOffline(a.network.offline)
	a.network.stop = mygo.Network.OnChanged(a.networkChanged)
}

func (a *app) networkChanged(status mygo.NetworkStatus) {
	if a.quitting {
		return
	}
	offline := !status.Online
	if a.network.known && a.network.offline == offline {
		return // metered-only changes need no reload
	}
	a.network.known, a.network.offline = true, offline
	netease.SetOffline(offline)
	if offline {
		a.pl.NetworkOffline()
	}
	if a.recovery.timer != nil {
		a.recovery.timer.Stop()
		a.recovery.timer = nil
	}
	if !offline && a.recovery.active {
		a.recovery.attempts = 0
		a.recoverLoads()
	}
	a.redraw()
}

func (a *app) refreshConnection() {
	a.networkChanged(readNetworkStatus())
	a.recovery.attempts = 0
	a.retryLoads(true) // explicit refresh also retries permanent failures
}
