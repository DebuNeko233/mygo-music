package viewmodel

// Page records an incrementally loaded collection, independently of its view controls.
type Page struct {
	Loaded  bool
	Loading bool
	More    bool
	Error   string
	Failure error
}
