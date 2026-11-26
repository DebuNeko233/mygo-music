/*
说明共享桌面控件包的职责，以及控件状态与构建期元素的所有权。
*/

// Package components contains MyGo controls shared by desktop features.
// Callers own each control's persistent state; elements remain build-scoped.
package components
