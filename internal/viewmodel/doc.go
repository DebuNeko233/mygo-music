// Package viewmodel defines loading state and refresh timing shared by features.
// Feature presentation state and commands live in its named subpackages.
//
// Commands and posted results run on one owning thread. Services perform I/O
// in workers and post results back to that thread; Post is asynchronous.
// The composition root supplies services and enforces account lifetime.
// Published fields and slices are borrowed read-only by views, not copied
// per frame. Windows, widgets, layout and graphics resources belong to views.
package viewmodel
