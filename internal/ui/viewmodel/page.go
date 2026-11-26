/*
定义增量加载页面的数据和状态，独立于视图控件。
*/

package viewmodel

// Page records an incrementally loaded collection, independently of its view controls.
type Page struct {
	Loaded  bool
	Loading bool
	More    bool
	Error   string
	Failure error
}
