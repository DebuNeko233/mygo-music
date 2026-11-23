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
	a.updateNetwork(status, true)
}

func (a *app) updateNetwork(status mygo.NetworkStatus, autoRecover bool) {
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
	} else {
		a.retryTrackArtwork(false)
		a.syncTrackArtwork()
	}
	if a.recovery.timer != nil {
		a.recovery.timer.Stop()
		a.recovery.timer = nil
	}
	if !offline && a.recovery.active && autoRecover {
		a.recovery.attempts = 0
		a.recoverLoads()
	}
	a.redraw()
}

func (a *app) refreshConnection() {
	// The explicit action owns this refresh. Reconnection must not start a
	// failure-only home request first and prevent the full refresh below.
	a.updateNetwork(readNetworkStatus(), false)
	a.recovery.attempts = 0
	if !a.network.offline && (a.route.Page == pageHome || a.route.Page == pageCharts) {
		a.loadHome()
	}
	a.retryLoads(true) // explicit refresh also retries permanent failures
}
