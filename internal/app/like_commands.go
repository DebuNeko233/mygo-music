package app

// likeCurrent likes the song or episode playing, or unlikes it, from the
// keyboard (⌘D), which shows no heart: a toast says which it did.
func (a *app) likeCurrent() {
	tr := a.pl.Current()
	if tr == nil || tr.IsLocal() {
		return
	}
	a.toggleLike(tr)
	switch {
	case a.account.Profile == nil:
		return // asked to sign in
	case !a.isLiked(tr):
		a.toast = "已取消喜欢"
	case tr.ProgramID != 0:
		a.toast = "已喜欢这期节目"
	default:
		a.toast = "已添加到我喜欢的音乐"
	}
}
