// Package viewmodel owns feature presentation state and user commands.
//
// Commands and posted results run on one owning thread. Services perform I/O
// in workers and post results back to that thread; Post is asynchronous.
// The composition root supplies services and enforces account lifetime.
// Published fields and slices are borrowed read-only by views, not copied
// per frame. Windows, widgets, layout and graphics resources belong to views.
package viewmodel
