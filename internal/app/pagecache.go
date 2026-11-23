package app

// searchBusy reports whether a search's page is still loading.
func searchBusy(d *searchData) bool {
	return d != nil && d.page.Loading
}
