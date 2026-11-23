package app

// podcastBusy reports whether a podcast's page is still loading.
func podcastBusy(d *podcastData) bool {
	return d != nil && (d.loading || d.page.Loading || d.subscribing)
}

// searchBusy reports whether a search's page is still loading.
func searchBusy(d *searchData) bool {
	return d != nil && d.page.Loading
}
